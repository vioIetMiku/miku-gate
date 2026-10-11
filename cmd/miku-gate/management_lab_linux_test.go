//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"mikugate/internal/management"
	"mikugate/internal/store"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLinuxManagementHTTPSCLI(t *testing.T) {
	if os.Getenv("MIKU_GATE_MANAGEMENT_TEST") != "1" {
		t.Skip("explicit real CLI HTTPS lab required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("control CLI must ordinary uid")
	}
	binary := os.Getenv("MIKU_GATE_BINARY")
	if !filepath.IsAbs(binary) {
		t.Fatal("exact CLI executable required")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	certdir := filepath.Join(dir, "certificate")
	identity, e := management.BootstrapIP("192.0.2.10", certdir)
	if e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(dir, "data")
	init := exec.Command(binary, "init", "--data", data)
	raw, e := init.Output()
	if e != nil {
		t.Fatal("init")
	}
	matches := regexp.MustCompile(`密码：([^\r\n]+)`).FindSubmatch(raw)
	if len(matches) != 2 {
		t.Fatal("one-time identity")
	}
	password := string(matches[1])
	reserve, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := reserve.Addr().String()
	reserve.Close()
	_, port, _ := net.SplitHostPort(address)
	origin := "https://192.0.2.10:" + port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve", "--data", data, "--listen", "127.0.0.1:"+port, "--public-origin", origin, "--tls-certificate", filepath.Join(certdir, "certificate.pem"), "--tls-key", filepath.Join(certdir, "private.key"), "--allow-bootstrap-certificate", "--control-plane-only")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		t.Fatal("start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cancel(); <-done })
	cert, _ := os.ReadFile(filepath.Join(certdir, "certificate.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "192.0.2.10", MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", "127.0.0.1:"+port)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(method, path, body, host, requestOrigin, cookie, csrf string) (int, []byte, []*http.Cookie) {
		r, _ := http.NewRequest(method, origin+path, strings.NewReader(body))
		r.Host = host
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if requestOrigin != "" {
			r.Header.Set("Origin", requestOrigin)
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(r)
		if e != nil {
			return 0, nil, nil
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 65536))
		return response.StatusCode, raw, response.Cookies()
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		code, _, _ := request("GET", "/api/health", "", "192.0.2.10:"+port, "", "", "")
		if code == 200 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
	code, body, cookies := request("POST", "/api/auth/login", string(login), "192.0.2.10:"+port, origin, "", "")
	if code != 200 || len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("real IP HTTPS login failed")
	}
	var session map[string]any
	json.Unmarshal(body, &session)
	cookie := cookies[0].Name + "=" + cookies[0].Value
	if code, _, _ := request("GET", "/api/exits", "", "evil.example:"+port, "", cookie, ""); code != 403 {
		t.Fatal("Host mismatch accepted")
	}
	if code, _, _ := request("POST", "/api/auth/logout", "{}", "192.0.2.10:"+port, "http://192.0.2.10:"+port, cookie, session["csrf"].(string)); code != 403 {
		t.Fatal("wrong scheme origin")
	}
	if code, _, _ := request("POST", "/api/auth/logout", "{}", "192.0.2.10:"+port, origin, cookie, session["csrf"].(string)); code != 204 {
		t.Fatal("logout")
	}
	if code, _, _ := request("GET", "/api/exits", "", "192.0.2.10:"+port, "", cookie, ""); code != 401 {
		t.Fatal("revocation")
	}
	if identity.SystemTrusted {
		t.Fatal("private IP bootstrap called system-trusted")
	}
	t.Log("Observed real ordinary UID CLI, IP SAN HTTPS verified by explicit certificate trust without TLS skip, exact Host/Origin, Secure session and logout/revocation. Not external public address reachability.")
}

func TestLinuxManagementHTTPSDomainCLI(t *testing.T) {
	if os.Getenv("MIKU_GATE_MANAGEMENT_TEST") != "1" {
		t.Skip("explicit real domain HTTPS CLI lab")
	}
	if os.Geteuid() == 0 {
		t.Fatal("ordinary UID required")
	}
	binary := os.Getenv("MIKU_GATE_BINARY")
	if !filepath.IsAbs(binary) {
		t.Fatal("exact binary")
	}
	root := t.TempDir()
	os.Chmod(root, 0700)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(11), Subject: pkix.Name{CommonName: "private HTTPS domain test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(72 * time.Hour)}
	caRaw, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(12), DNSNames: []string{"management.test"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(leafKey)
	cp, kp, cap := filepath.Join(root, "certificate.pem"), filepath.Join(root, "private.key"), filepath.Join(root, "ca.pem")
	os.WriteFile(cp, append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})...), 0600)
	os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600)
	os.WriteFile(cap, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw}), 0600)
	data := filepath.Join(root, "data")
	output, err := exec.Command(binary, "init", "--data", data).Output()
	if err != nil {
		t.Fatal("init")
	}
	matches := regexp.MustCompile(`密码：([^\r\n]+)`).FindSubmatch(output)
	if len(matches) != 2 {
		t.Fatal("one-time credential")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	origin := "https://management.test:" + port
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "serve", "--data", data, "--listen", "127.0.0.1:"+port, "--public-origin", origin, "--tls-certificate", cp, "--tls-key", kp, "--control-plane-only")
	// Trust root injected before x509 initialization only into this child CLI,
	// never host trust store, and no allow-bootstrap or insecure flag.
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+cap, "SSL_CERT_DIR="+filepath.Join(root, "empty"))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		cancel()
		t.Fatal("serve")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			cancel()
			<-done
		}
	})
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw}))
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "management.test", MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", "127.0.0.1:"+port)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := client.Get(origin + "/api/health")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": string(matches[1])})
	request, _ := http.NewRequest("POST", origin+"/api/auth/login", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("domain TLS request")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || len(response.Cookies()) == 0 || !response.Cookies()[0].Secure {
		t.Fatal("domain TLS login/secure cookie")
	}

	// Exercise the actual installed-account TLS provider through authenticated
	// APIs. Neither PEM nor credential values are included in test output.
	loginBody, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	if err != nil {
		t.Fatal("read login")
	}
	var session map[string]string
	if json.Unmarshal(loginBody, &session) != nil {
		t.Fatal("login session")
	}
	cookie := response.Cookies()[0]
	call := func(method, path string, input any) (int, []byte) {
		var encoded []byte
		if input != nil {
			encoded, _ = json.Marshal(input)
		}
		req, _ := http.NewRequest(method, origin+path, strings.NewReader(string(encoded)))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", session["csrf"])
		res, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 65536))
		return res.StatusCode, raw
	}
	code, meta := call("POST", "/api/ingress/certificate", map[string]any{"hostname": "management.test", "confirm_read": true})
	if code != 200 || !strings.Contains(string(meta), `"selected_provider":"management-domain-copy"`) || strings.Contains(string(meta), "PRIVATE KEY") {
		t.Fatal("ordinary control did not reuse verified management certificate without leaking private key")
	}
	_, asset, err := management.DomainAsset(mustManagementEndpoint(t, origin), cp, kp, "management.test")
	// The test process itself has not imported the child-only test CA; this
	// read must remain unverified, not inherit trust from an HTTP success flag.
	if err != nil || asset != nil {
		t.Fatal("child trust wrongly transferred to test coordinator")
	}
	for _, bad := range []string{"other.test", "127.0.0.1"} {
		if _, _, err := management.DomainAsset(mustManagementEndpoint(t, origin), cp, kp, bad); err == nil {
			t.Fatal("cross-identity reuse")
		}
	}
	code, made := call("POST", "/api/exits", map[string]any{"name": "TLS certificate reuse lab", "country": "JP"})
	if code != 201 {
		t.Fatal("create logical exit")
	}
	var created struct {
		Exit struct {
			ID string `json:"id"`
		} `json:"exit"`
		Password string `json:"password"`
	}
	if json.Unmarshal(made, &created) != nil {
		t.Fatal("created response")
	}
	time.Sleep(3100 * time.Millisecond) // explicit file-operation throttle
	code, saved := call("PUT", "/api/exits/"+created.Exit.ID+"/ingress", map[string]any{
		"request":       map[string]any{"core": "xray", "template": "vless-tls-vision", "address": "management.test", "listen": "127.0.0.1", "port": 28443, "server_name": "management.test"},
		"exit_password": created.Password, "confirm": true, "confirm_read": true,
	})
	if code != 200 || !strings.Contains(string(saved), `"configured":true`) || strings.Contains(string(saved), "PRIVATE KEY") {
		t.Fatal("save domain TLS entrance")
	}
	code, imported := call("POST", "/api/exits/"+created.Exit.ID+"/ingress/client", map[string]any{"confirm": true})
	if code != 200 || !strings.Contains(string(imported), "vless://") || strings.Contains(string(imported), "PRIVATE KEY") || strings.Contains(string(imported), created.Password) {
		t.Fatal("safe TLS client import")
	}
	// Real ordinary CLI refresh after stopping the lease owner. The private CA
	// remains scoped to child processes; no system trust or public ACME mutation.
	initialDB, err := store.Open(filepath.Join(data, "miku-gate.db"))
	if err != nil {
		t.Fatal("inspect initial material")
	}
	initialMaterial, _, _, err := initialDB.Managed(created.Exit.ID)
	instance, instanceErr := initialDB.InstanceID()
	initialDB.Close()
	if err != nil || instanceErr != nil {
		t.Fatal("initial encrypted TLS identity")
	}
	oldDigest := sha256.Sum256(der)
	rotateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rotateLeaf := *leaf
	rotateLeaf.SerialNumber = big.NewInt(13)
	rotateDER, err := x509.CreateCertificate(rand.Reader, &rotateLeaf, ca, &rotateKey.PublicKey, key)
	if err != nil {
		t.Fatal("new domain certificate")
	}
	rotateEncoded, _ := x509.MarshalPKCS8PrivateKey(rotateKey)
	rotateCP, rotateKP := filepath.Join(root, "rotated-certificate.pem"), filepath.Join(root, "rotated-private.key")
	rotateCertificate := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rotateDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})...)
	os.WriteFile(rotateCP, rotateCertificate, 0600)
	os.WriteFile(rotateKP, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rotateEncoded}), 0600)
	cancel()
	<-done
	stopped = true
	transport.CloseIdleConnections()
	refresh := exec.Command(binary, "management", "refresh-ingress", "--confirm", "--data="+data, "--expected-instance="+instance, "--expected-leaf="+hex.EncodeToString(oldDigest[:]), "--origin="+origin, "--certificate="+rotateCP, "--key="+rotateKP)
	refresh.Env = append(os.Environ(), "SSL_CERT_FILE="+cap, "SSL_CERT_DIR="+filepath.Join(root, "empty"))
	refreshed, err := refresh.Output()
	if err != nil || !strings.Contains(string(refreshed), `"updated_ingress_count":1`) || !strings.Contains(string(refreshed), `"identities_preserved":true`) || strings.Contains(string(refreshed), "PRIVATE KEY") || bytes.Contains(refreshed, []byte(created.Password)) {
		t.Fatal("actual stopped ordinary CLI certificate refresh")
	}
	renewedDB, err := store.Open(filepath.Join(data, "miku-gate.db"))
	if err != nil {
		t.Fatal("inspect refreshed material")
	}
	renewedMaterial, _, _, err := renewedDB.Managed(created.Exit.ID)
	renewedDB.Close()
	if err != nil || renewedMaterial.Identity != initialMaterial.Identity || renewedMaterial.Proxy != initialMaterial.Proxy || renewedMaterial.Request != initialMaterial.Request || !bytes.Equal(renewedMaterial.CertificatePEM, rotateCertificate) {
		t.Fatal("renewal changed identities/bindings or did not persist certificate")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel() // Also explicit for vet: cleanup closes the current restarted process.
	cmd = exec.CommandContext(ctx, binary, "serve", "--data", data, "--listen=127.0.0.1:"+port, "--public-origin="+origin, "--tls-certificate="+rotateCP, "--tls-key="+rotateKP)
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+cap, "SSL_CERT_DIR="+filepath.Join(root, "empty"))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		cancel()
		t.Fatal("restart rotated domain HTTPS")
	}
	done = make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped = false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, e := client.Get(origin + "/api/health")
		if e == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	code, rotatedImport := call("POST", "/api/exits/"+created.Exit.ID+"/ingress/client", map[string]any{"confirm": true})
	if code != 200 || !bytes.Equal(imported, rotatedImport) {
		t.Fatal("actual rotated HTTPS did not preserve client import")
	}
	if code, _ := call("DELETE", "/api/exits/"+created.Exit.ID+"/ingress", nil); code != 204 {
		t.Fatal("remove temporary ingress")
	}
	if code, _ := call("DELETE", "/api/exits/"+created.Exit.ID, nil); code != 204 {
		t.Fatal("remove temporary exit")
	}
	t.Log("Observed actual domain HTTPS CLI and same-domain managed certificate reuse by ordinary account, authenticated encrypted TLS entrance save/client import without PEM or exit password leakage; process-only private CA, no bootstrap/insecure or host trust mutation. Actual stopped ordinary CLI certificate refresh, encrypted material and client identity preserved, rotated HTTPS/client import passed. Public ACME/DNS ownership and data forwarding not tested.")

}

func mustManagementEndpoint(t *testing.T, origin string) management.Endpoint {
	t.Helper()
	ep, err := management.ParseOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}
	return ep
}
