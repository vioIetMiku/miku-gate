package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDev35ActualProtocolStack(t *testing.T) {
	for _, tc := range []struct{ name, protocol, stream, clients, want string }{
		{"Vision TLS", "vless", `{"network":"tcp","security":"tls"}`, `{"clients":[{"flow":"xtls-rprx-vision"}]}`, "VLESS + TCP + TLS + VISION"},
		{"encoded reality", "vless", `"{\"network\":\"raw\",\"security\":\"reality\"}"`, `"{\"clients\":[{\"flow\":\"xtls-rprx-vision\"}]}"`, "VLESS + TCP + REALITY + VISION"},
		{"ordinary TLS", "vless", `{"network":"tcp","security":"tls"}`, `{"clients":[{"flow":""}]}`, "VLESS + TCP + TLS"},
		{"disabled flow", "vless", `{"network":"tcp","security":"tls"}`, `{"clients":[{"flow":"xtls-rprx-vision","enable":false}]}`, "VLESS + TCP + TLS"},
		{"mixed clients", "vless", `{"network":"tcp","security":"tls"}`, `{"clients":[{"flow":"xtls-rprx-vision"},{"flow":""}]}`, "VLESS + TCP + TLS + VISION（可选）"},
		{"websocket", "vless", `{"network":"ws","security":"tls"}`, `{"clients":[{"flow":"xtls-rprx-vision"}]}`, "VLESS + WS + TLS"},
		{"Trojan", "trojan", `{"network":"grpc","security":"tls"}`, `{}`, "TROJAN + GRPC + TLS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inboundStackJSON(tc.protocol, json.RawMessage(tc.stream), json.RawMessage(tc.clients)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	n := &Native{store: &nativeStore{Inbounds: []*nativeInbound{{ID: 1, Protocol: "vless", Network: "tcp", Security: "reality", Enable: true, Clients: []nativeClient{{Enable: true, Flow: "xtls-rprx-vision"}}}}}}
	list, err := n.Inbounds(nil)
	if err != nil || len(list) != 1 || list[0].Stack != "VLESS + TCP + REALITY + VISION" {
		t.Fatalf("native stack missing: %+v, %v", list, err)
	}
}

func TestDev35CertificateWebrootArguments(t *testing.T) {
	root := t.TempDir()
	input := domainRequest{Domain: "node.example.com", Email: "ops@example.com", Webroot: root}
	args, err := certificateCommandArgs(input)
	if err != nil || !strings.Contains(strings.Join(args, " "), "--webroot -w "+root) || strings.Contains(strings.Join(args, " "), "--standalone") {
		t.Fatal(args, err)
	}
	input.Webroot = "/"
	if _, err = certificateCommandArgs(input); err == nil {
		t.Fatal("root directory accepted")
	}
	alias := filepath.Join(root, "alias")
	if err = os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	input.Webroot = alias
	if _, err = certificateCommandArgs(input); err == nil {
		t.Fatal("symlink directory accepted")
	}
	input.Webroot = ""
	args, err = certificateCommandArgs(input)
	if err != nil || args[1] != "--standalone" {
		t.Fatal(args, err)
	}
}

func TestDev35IssueReusesMatchingCertificateBeforeEmailOrPortCheck(t *testing.T) {
	root := t.TempDir()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fixture CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "node.example.com"}, DNSNames: []string{"node.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, caCert, pub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	certFile, keyFile := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	d := &DomainRegistry{dir: root, values: []RegisteredDomain{{Domain: "node.example.com", CertFile: certFile, KeyFile: keyFile}}}
	r := httptest.NewRequest(http.MethodPost, "/api/domains", strings.NewReader(`{"action":"issue","domain":"node.example.com","management":false}`))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("existing certificate not reused: %d %s", w.Code, w.Body.String())
	}
	if len(d.values) != 1 || d.values[0].SourceCert != certFile {
		t.Fatal("wrong certificate source saved")
	}
}

func TestDev35HTTPDomainReturnsFullLoginAddress(t *testing.T) {
	root := t.TempDir()
	old, oldPath := getWebSettings(), webSettingsPath
	oldBase := currentBasePath()
	defer func() {
		webSettingsMu.Lock()
		webSettingsCur = old
		webSettingsPath = oldPath
		webSettingsMu.Unlock()
		basePathMu.Lock()
		basePathCur = oldBase
		basePathMu.Unlock()
	}()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	webSettingsMu.Lock()
	webSettingsPath = filepath.Join(root, "settings.json")
	webSettingsCur = WebSettings{Port: port, ListenAddr: "127.0.0.1", PublicIP: "192.0.2.20"}
	webSettingsMu.Unlock()
	basePathMu.Lock()
	basePathCur = "/fixture-login"
	basePathMu.Unlock()
	server := newWebServer(http.NotFoundHandler())
	defer func() {
		if server.srv != nil {
			server.srv.Close()
		}
	}()
	d := &DomainRegistry{dir: root, server: server}
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/domains", strings.NewReader(`{"action":"bind-http","domain":"node.example.com"}`)))
	var result struct {
		URL        string `json:"url"`
		Management bool   `json:"management"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	want := "http://" + net.JoinHostPort("node.example.com", fmtInt(port)) + "/fixture-login/"
	if w.Code != 200 || result.URL != want || !result.Management {
		t.Fatal(w.Code, result)
	}
	if cfg := getWebSettings(); cfg.TLSCert != "" || cfg.PublicHost != "node.example.com" {
		t.Fatal(cfg)
	}
}

func fmtInt(value int) string { raw, _ := json.Marshal(value); return string(raw) }

func TestDev35JobLoginReceiptAndReadinessIdentity(t *testing.T) {
	job := (&JobStore{}).New("domain", []string{"activate"})
	job.Set(0, "ok", "ready")
	job.SetResultURL("https://node.example.com:32456/secret/")
	job.Finish()
	encoded, _ := json.Marshal(job.View())
	if !bytes.Contains(encoded, []byte(`"result_url":"https://node.example.com:32456/secret/"`)) {
		t.Fatal("login receipt missing")
	}
	auth := &Auth{sessions: map[string]time.Time{}}
	w := httptest.NewRecorder()
	auth.Wrap(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Header().Get("X-Miku-Gate") != "1" {
		t.Fatal("installer cannot distinguish another panel")
	}
}

func TestDev35IPv6OccupiedPortsAreReserved(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::]:0")
	if err != nil {
		t.Skip("IPv6 unavailable")
	}
	defer ln.Close()
	if portAvailable(ln.Addr().(*net.TCPAddr).Port) {
		t.Fatal("IPv6-only listener was missed")
	}
}
