//go:build linux

package coreruntime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"mikugate/internal/certassets"
	"mikugate/internal/ingress"
	"mikugate/internal/proxy"
	"mikugate/internal/security"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLinuxManagedNativeRuntime(t *testing.T) {
	if os.Getenv("MIKU_GATE_INGRESS_TEST") != "1" {
		t.Skip("explicit private core runtime lab required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("native core must be nonroot")
	}
	links, e := net.Interfaces()
	if e != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("private loopback network required")
	}
	xray, sing := os.Getenv("MIKU_GATE_XRAY"), os.Getenv("MIKU_GATE_SINGBOX")
	if !filepath.IsAbs(xray) || !filepath.IsAbs(sing) {
		t.Fatal("exact core paths")
	}
	// Custom test CA is scoped to this process; no host trust change.
	dir := t.TempDir()
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private runtime CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(72 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caRaw, _ := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"runtime.test"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	leafRaw, _ := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	keyRaw, _ := x509.MarshalPKCS8PrivateKey(leafKey)
	certPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafRaw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})...)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw})
	caFile := filepath.Join(dir, "ca.pem")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw}), 0600)
	t.Setenv("SSL_CERT_FILE", caFile)
	t.Setenv("SSL_CERT_DIR", filepath.Join(dir, "empty"))
	asset, e := certassets.Validate("runtime.test", certPEM, keyPEM)
	if e != nil {
		t.Fatal("test cert")
	}
	for _, core := range []string{"xray", "sing-box"} {
		t.Run(core, func(t *testing.T) {
			frontendLN, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			frontendPort := frontendLN.Addr().(*net.TCPAddr).Port
			exitLife, stopExit := context.WithCancel(context.Background())
			done := make(chan error, 1)
			server := &proxy.Server{Authenticate: func(u, p string) bool { return u == "test-user" && p == "test-password" }, Dial: func(context.Context, string, string) (net.Conn, error) {
				a, b := net.Pipe()
				go func() { defer b.Close(); io.Copy(b, b) }()
				return a, nil
			}}
			go func() { done <- server.Serve(exitLife, frontendLN) }()
			defer func() { stopExit(); <-done }()
			reservation, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			port := reservation.Addr().(*net.TCPAddr).Port
			reservation.Close()
			request := ingress.ManagedRequest{Core: core, Template: "vless-tls-vision", Address: "127.0.0.1", Listen: "127.0.0.1", Port: port, ServerName: "runtime.test"}
			bundle, e := ingress.GenerateManaged(request, ingress.Proxy{Port: frontendPort, Username: "test-user", Password: "test-password"}, asset, ingress.ManagedFiles{Certificate: "/unused/cert", PrivateKey: "/unused/key"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			hash, _ := security.HashPassword("test-password")
			source := sourceFixture{material: *bundle.Material, hash: hash}
			source.exit.ID = "exit"
			source.exit.Port = frontendPort
			source.exit.Username = "test-user"
			manager, e := New(context.Background(), source, exitFixture{life: exitLife, active: true}, Options{XrayPath: xray, SingBoxPath: sing})
			if e != nil {
				t.Fatal(e)
			}
			defer manager.Close(context.Background())
			state, e := manager.Start(context.Background(), "exit")
			if e != nil {
				t.Fatal("native runner failed stdin/check/owned socket", e)
			}
			if !state.ListenerOwned || !state.CoreValidated || state.DataVerified || state.PID <= 0 {
				t.Fatal("wrong native scope")
			}
			// Derive selected UUID from generated CLIENT material, not server private state.
			var client map[string]any
			json.Unmarshal(bundle.ClientJSON, &client)
			out := client["outbounds"].([]any)[0].(map[string]any)
			uuid := ""
			if core == "xray" {
				settings := out["settings"].(map[string]any)
				next := settings["vnext"].([]any)[0].(map[string]any)
				uuid = next["users"].([]any)[0].(map[string]any)["id"].(string)
			} else {
				uuid = out["uuid"].(string)
			}
			// TLS ownership handshake is checked from the generated listener itself; the
			// protocol data-flow lab already verifies Vision framing with actual clients.
			roots := x509.NewCertPool()
			roots.AppendCertsFromPEM(certPEM)
			c, e := tls.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &tls.Config{ServerName: "runtime.test", RootCAs: roots, MinVersion: tls.VersionTLS12})
			if e != nil || uuid == "" {
				t.Fatal("owned generated TLS listener unavailable")
			}
			c.Close()
			if e = manager.WithStopped("exit", func() error { t.Fatal("live config mutation"); return nil }); e == nil {
				t.Fatal("missing active ingress lock")
			}
			stopExit()
			limit := time.Now().Add(3 * time.Second)
			for time.Now().Before(limit) {
				if _, ok := manager.Snapshot("exit"); !ok {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, ok := manager.Snapshot("exit"); ok {
				t.Fatal("exit lease did not revoke core")
			}
			conn, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
			if e == nil {
				conn.Close()
				t.Fatal("revoked entrance still listening")
			}
			t.Logf("Observed %s: production native check/stdin embedded TLS certificate, nonroot/no_new_privs pinned core, actual PID socket ownership, generated TLS handshake, mutation refusal and exit lease revokes child/listener. Not a public VPN full path.", core)
		})
	}
}
