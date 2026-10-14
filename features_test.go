package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebProtocolSwitchKeepsPortAndRejectsBadCertificate(t *testing.T) {
	cert, key := testCertificate(t, t.TempDir(), "web.example.com")
	port := freePort(t)
	cfg := WebSettings{ListenAddr: "127.0.0.1", Port: port}
	s := newWebServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ready")) }))
	defer func() {
		if s.srv != nil {
			s.srv.Close()
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // Local, temporary self-signed test certificate.
	check := func(scheme string) {
		t.Helper()
		response, err := client.Get(scheme + "://127.0.0.1:" + strconv.Itoa(port))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal(response.Status)
		}
	}
	if err := s.reload(cfg); err != nil {
		t.Fatal(err)
	}
	check("http")
	cfg.TLSCert, cfg.TLSKey = cert, key
	if err := s.reload(cfg); err != nil {
		t.Fatal(err)
	}
	check("https")
	bad := cfg
	bad.TLSKey = filepath.Join(t.TempDir(), "missing.key")
	if s.reload(bad) == nil {
		t.Fatal("missing key accepted")
	}
	check("https")
	cfg.TLSCert, cfg.TLSKey = "", ""
	if err := s.reload(cfg); err != nil {
		t.Fatal(err)
	}
	check("http")
}

func testCertificate(t *testing.T, root, name string) (string, string) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	cert, keypath := filepath.Join(root, name+".pem"), filepath.Join(root, name+".key")
	if err = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keypath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	return cert, keypath
}

func TestDomainCertificateSelectionAndPersistence(t *testing.T) {
	root := t.TempDir()
	certA, keyA := testCertificate(t, root, "a.example.com")
	certB, keyB := testCertificate(t, root, "b.example.com")
	d := &DomainRegistry{dir: root, values: []RegisteredDomain{{Domain: "a.example.com", CertFile: certA, KeyFile: keyA}, {Domain: "b.example.com", CertFile: certB, KeyFile: keyB}}}
	choices := d.Catalog()
	var selected CertificateChoice
	for _, c := range choices {
		if c.CertFile == certB {
			selected = c
		}
	}
	if selected.ID == "" {
		t.Fatal("second certificate missing")
	}
	value, domain, err := d.resolveTLS(selected.ID, "b.example.com")
	if err != nil || domain != "b.example.com" || value.CertFile != certB {
		t.Fatal("wrong certificate chosen", err)
	}
	if _, _, err = d.resolveTLS(selected.ID, "a.example.com"); err == nil {
		t.Fatal("wrong SNI accepted")
	}
	entry, err := d.keepCertificate(selected, "b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = certificateChoice(entry.CertFile, entry.KeyFile, "test"); err != nil {
		t.Fatal(err)
	}
	reloaded := newDomainRegistry(root, nil, nil)
	if len(reloaded.values) != 2 {
		t.Fatal("domain choices lost")
	}
	previous := certificateRegistry
	certificateRegistry = reloaded
	defer func() { certificateRegistry = previous }()
	ib := &nativeInbound{Protocol: "vless", Security: "tls", TLS: &tlsConfig{ServerName: "b.example.com"}, Port: 30001}
	link := shareLink(ib, nativeClient{ID: newUUID()}, "a.example.com")
	if !strings.Contains(link, "@b.example.com:30001") {
		t.Fatal("chosen domain not used by link", link)
	}
	raw, _ := json.Marshal(selected)
	if strings.Contains(string(raw), keyB) || strings.Contains(string(raw), certB) {
		t.Fatal("private certificate paths exposed")
	}
}

func TestUnlimitedSlotsPreserveOldSubnets(t *testing.T) {
	m := NewManager(0, t.TempDir())
	seen := map[string]bool{}
	for i := 1; i <= 1000; i++ {
		slot, err := m.freeSlot()
		if err != nil || slot != i {
			t.Fatal("software count limit remains", i, slot, err)
		}
		tunnel := &Tunnel{Slot: slot}
		host, peer, cidr, err := tunnel.linkAddresses()
		if err != nil || seen[cidr] || host == peer {
			t.Fatal("subnet collision", slot, cidr, err)
		}
		seen[cidr] = true
		m.tunnels[slot] = tunnel
		if slot <= 255 && cidr != tunnel.subnet()+".0/30" {
			t.Fatal("legacy subnet changed")
		}
	}
}

func TestVPNProfilesRejectExecutableAndFileDirectives(t *testing.T) {
	for _, raw := range []string{"client\nplugin /tmp/evil.so", "client\n--up /bin/sh", "client\nconfig /etc/shadow", "client\nlog /etc/passwd", "client\n<connection>\nremote 1.2.3.4\n</connection>"} {
		if validateVPNProfile(raw) == nil {
			t.Fatal("unsafe profile accepted", raw)
		}
	}
	if err := validateVPNProfile("client\ndev tun\nproto tcp\nremote 1.2.3.4 443\n<ca>\ncertificate data\n</ca>\n"); err != nil {
		t.Fatal(err)
	}
}

func TestIPInspectorPreservesMissingFlagsAndRateBackoff(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if strings.HasPrefix(r.URL.Path, "/rate") {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/who") {
			w.Write([]byte(`{"success":true,"ip":"8.8.8.8","connection":{"asn":15169,"org":"Google LLC","isp":"Google LLC"},"country":"United States"}`))
		} else {
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	s := &IPInspector{cache: map[string]IPInfo{}, pending: map[string]bool{}, blocked: map[string]time.Time{}, client: server.Client(), dir: t.TempDir(), ipapi: server.URL + "/bad/", whoAPI: server.URL + "/who/"}
	s.lookup("8.8.8.8")
	value := s.get("8.8.8.8")
	if value.ASN != 15169 || value.Hosting != nil || value.Mobile != nil || value.IPType != "datacenter" || value.TypeConfidence != "low" {
		t.Fatal("missing flags were fabricated or provider inference was not marked", value)
	}
	_, _ = s.request("limited", "GET", server.URL+"/rate", nil)
	before := count
	_, _ = s.request("limited", "GET", server.URL+"/rate", nil)
	if count != before {
		t.Fatal("429 backoff ignored")
	}
}

func TestPublicBrandArtworkIsAvailableBeforeLogin(t *testing.T) {
	a, _, err := NewAuth(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/assets/", brandAssets())
	recorder := httptest.NewRecorder()
	a.Wrap(mux).ServeHTTP(recorder, httptest.NewRequest("GET", "/assets/miku-hero-v1.png", nil))
	if recorder.Code != 200 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "image/") {
		t.Fatal("login illustration requires login")
	}
}
