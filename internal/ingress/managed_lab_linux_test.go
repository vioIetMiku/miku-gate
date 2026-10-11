//go:build linux

package ingress

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mikugate/internal/certassets"
	"mikugate/internal/proxy"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestLinuxManagedCoreTemplates(t *testing.T) {
	if os.Getenv("MIKU_GATE_INGRESS_TEST") != "1" {
		t.Skip("explicit loopback-only real core lab required")
	}
	links, e := net.Interfaces()
	if e != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("isolated loopback namespace required")
	}
	xray, sing := os.Getenv("MIKU_GATE_XRAY"), os.Getenv("MIKU_GATE_SINGBOX")
	if !filepath.IsAbs(xray) || !filepath.IsAbs(sing) {
		t.Fatal("exact core paths required")
	}
	dir := t.TempDir()
	cert, key, ca := labCertificates(t, dir)
	t.Setenv("SSL_CERT_FILE", ca)
	t.Setenv("SSL_CERT_DIR", filepath.Join(dir, "empty-cert-directory"))
	certRaw, _ := os.ReadFile(cert)
	keyRaw, _ := os.ReadFile(key)
	asset, e := certassets.Validate("ingress.test", certRaw, keyRaw)
	if e != nil {
		t.Fatal("private process-only certificate asset verification failed")
	}
	pair, e := tls.LoadX509KeyPair(cert, key)
	if e != nil {
		t.Fatal("fixture certificate")
	}
	cover, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	tlsCover := tls.NewListener(cover, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}, NextProtos: []string{"h2", "http/1.1"}})
	defer tlsCover.Close()
	go func() {
		for {
			conn, e := tlsCover.Accept()
			if e != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				if secured, ok := conn.(*tls.Conn); ok {
					secured.Handshake()
				}
				io.Copy(io.Discard, conn)
			}()
		}
	}()
	reality := RealityTarget{Hostname: "ingress.test", Address: "127.0.0.1", Port: cover.Addr().(*net.TCPAddr).Port, CheckedAt: time.Now(), NotAfter: asset.Summary().NotAfter, TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true, verified: true, NetworkPath: "private loopback test target; not public candidate"}
	for _, tc := range []struct{ core, binary string }{{"xray", xray}, {"sing-box", sing}} {
		for _, template := range Templates() {
			t.Run(tc.core+"/"+template.ID, func(t *testing.T) {
				var targetCalls, dialCalls, authCalls atomic.Int32
				target, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer target.Close()
				targetPort := target.Addr().(*net.TCPAddr).Port
				go func() {
					for {
						c, e := target.Accept()
						if e != nil {
							return
						}
						targetCalls.Add(1)
						go func() { defer c.Close(); c.SetDeadline(time.Now().Add(4 * time.Second)); io.Copy(c, c) }()
					}
				}()
				udp, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
				if e != nil {
					t.Fatal(e)
				}
				defer udp.Close()
				var udpCalls atomic.Int32
				go func() {
					b := make([]byte, 1024)
					for {
						n, from, e := udp.ReadFromUDP(b)
						if e != nil {
							return
						}
						udpCalls.Add(1)
						udp.WriteToUDP(b[:n], from)
					}
				}()
				ln, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				proxyCtx, stopProxy := context.WithCancel(context.Background())
				proxyDone := make(chan error, 1)
				frontend := &proxy.Server{Authenticate: func(u, p string) bool {
					authCalls.Add(1)
					return u == "managed-lab-user" && p == "managed-lab-password"
				}, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
					host, port, e := net.SplitHostPort(a)
					if e != nil || port != strconv.Itoa(targetPort) || (host != "managed-target.invalid" && host != "127.0.0.1") {
						return nil, fmt.Errorf("private target outside proof")
					}
					dialCalls.Add(1)
					return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Addr().String())
				}}
				go func() { proxyDone <- frontend.Serve(proxyCtx, ln) }()
				t.Cleanup(func() { stopProxy(); <-proxyDone })
				serverPort, clientPort := reservePort(t), reservePort(t)
				request := ManagedRequest{Core: tc.core, Template: template.ID, Address: "127.0.0.1", Listen: "127.0.0.1", Port: serverPort, ServerName: "ingress.test"}
				var certAsset *certassets.Asset
				var checkedTarget *RealityTarget
				if template.NeedsCertificate {
					certAsset = asset
				} else {
					request.RealityHostname = "ingress.test"
					checkedTarget = &reality
				}
				bundle, e := GenerateManaged(request, Proxy{ln.Addr().(*net.TCPAddr).Port, "managed-lab-user", "managed-lab-password"}, certAsset, ManagedFiles{cert, key}, checkedTarget)
				if e != nil {
					t.Fatal(e)
				}
				subdir := t.TempDir()
				serverFile, clientFile := filepath.Join(subdir, "server.json"), filepath.Join(subdir, "client.json")
				if os.WriteFile(serverFile, bundle.ServerJSON, 0600) != nil {
					t.Fatal("write private generated config")
				}
				// Only the local client SOCKS listen port is adapted for parallel fixtures;
				// endpoint/security/UUID/REALITY credentials are exactly generated client data.
				clientMap, e := parseJSON(bundle.ClientJSON)
				if e != nil {
					t.Fatal(e)
				}
				in := object(array(clientMap["inbounds"])[0])
				if tc.core == "xray" {
					in["port"] = clientPort
				} else {
					in["listen_port"] = clientPort
				}
				clientRaw, _ := json.Marshal(clientMap)
				os.WriteFile(clientFile, clientRaw, 0600)
				for _, file := range []string{serverFile, clientFile} {
					check := coreCommand(context.Background(), tc.binary, tc.core, file, true)
					if e := check.Run(); e != nil {
						t.Fatal("actual core rejected generated server/client template")
					}
				}
				startCore(t, tc.binary, tc.core, serverFile, ca, serverPort)
				startCore(t, tc.binary, tc.core, clientFile, ca, clientPort)
				if !echoThrough(t, clientPort, "managed-target.invalid", targetPort) || dialCalls.Load() != 1 || authCalls.Load() != 1 {
					t.Fatal("generated encrypted client/server did not reach authenticated Miku frontend")
				}
				if udpThrough(t, clientPort, udp.LocalAddr().(*net.UDPAddr)) || udpCalls.Load() != 0 {
					t.Fatal("managed template leaked UDP direct")
				}
				before := targetCalls.Load()
				stopProxy()
				ln.Close()
				if echoThrough(t, clientPort, "127.0.0.1", targetPort) || targetCalls.Load() != before {
					t.Fatal("generated template used direct fallback after exit loss")
				}
				t.Logf("Observed %s/%s: actual generated server+client checks, random client credentials, strict private CA TLS or generated REALITY X25519 key/shortID authenticated handshake, unresolved business host reached real Miku authenticated SOCKS, UDP drop, stopped Miku exit no direct fallback. No existing service/configuration applied.", tc.core, template.ID)
			})
		}
	}
}
