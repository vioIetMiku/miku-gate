package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func quickFixtureJob(m *Manager) *Job {
	labels := make([]string, quickSummaryStep+1)
	return m.jobs.New("quick fixture", labels)
}

func quickFixtureNodes() []Node {
	var nodes []Node
	for r, region := range quickRegions {
		for i := 1; i <= 8; i++ {
			nodes = append(nodes, Node{HostName: fmt.Sprintf("%s-%d", region.Code, i), CountryCode: region.Code, IP: fmt.Sprintf("198.51.%d.%d", 100+r, i)})
		}
	}
	return nodes
}

func TestQuickSetupExactCountryPlanAndUniqueEndpoints(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = quickFixtureNodes()
	// A duplicate endpoint in another country must never be picked twice.
	m.nodes = append([]Node{{HostName: "duplicate", CountryCode: "KR", IP: m.nodes[0].IP}}, m.nodes...)
	m.tunnels[1] = &Tunnel{Slot: 1, Node: Node{HostName: "occupied", IP: m.nodes[1].IP}, Status: "up"}
	targets := m.quickTargets(quickFixtureJob(m), 77)
	picks := m.quickRacePicks(targets)
	counts, used := map[string]int{}, map[string]bool{}
	if len(targets) != 11 {
		t.Fatal("wrong default target count", len(targets))
	}
	for _, node := range picks {
		if used[node.IP] || node.IP == m.tunnels[1].Node.IP {
			t.Fatal("duplicate or occupied endpoint selected", node)
		}
		used[node.IP] = true
	}
	for _, target := range targets {
		counts[target.code]++
	}
	if counts["JP"] != 5 || counts["KR"] != 3 || counts["US"] != 3 {
		t.Fatal("wrong country distribution", counts)
	}
}

func TestQuickSetupShortagesAndSlotLimitsAreReported(t *testing.T) {
	useSettings(t)
	for _, limit := range []int{0, 2} {
		m := NewManager(limit, t.TempDir())
		m.nodes = []Node{{HostName: "jp", CountryCode: "JP"}, {HostName: "kr", CountryCode: "KR"}, {HostName: "us", CountryCode: "US"}, {HostName: "de", CountryCode: "DE"}}
		job := quickFixtureJob(m)
		targets := m.quickTargets(job, 1)
		picks := m.quickRacePicks(targets)
		want := 11
		if limit > 0 {
			want = limit
		}
		if len(targets) != want {
			t.Fatal("slot limit was not respected", limit, targets)
		}
		failed := 0
		for _, step := range job.View().Steps[quickExitStep:quickSummaryStep] {
			if step.Status == "failed" {
				failed++
			}
		}
		if failed != 11-want {
			t.Fatal("missing targets were not reported", failed)
		}
		for _, pick := range picks {
			if pick.CountryCode == "DE" {
				t.Fatal("country shortage silently used another country")
			}
		}
	}
}

func TestQuickSetupRepeatedRunOnlyTopsUpSameTemplate(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = quickFixtureNodes()
	for i := 0; i < 2; i++ {
		m.tunnels[i+1] = &Tunnel{Slot: i + 1, Node: m.nodes[i], TemplateID: 5, Status: "up", EntryReady: true}
	}
	// Another configuration is preserved and does not count towards this one.
	m.tunnels[3] = &Tunnel{Slot: 3, Node: m.nodes[8], TemplateID: 9, Status: "up", EntryReady: true}
	targets := m.quickTargets(quickFixtureJob(m), 5)
	picks := m.quickRacePicks(targets)
	if len(targets) != 9 {
		t.Fatal("expected only nine missing targets", len(targets))
	}
	for i, target := range targets {
		for j, pick := range picks {
			if pick.CountryCode != target.code {
				continue
			}
			m.tunnels[i+4] = &Tunnel{Slot: i + 4, Node: pick, TemplateID: 5, Status: "up", EntryReady: true}
			picks = append(picks[:j], picks[j+1:]...)
			break
		}
	}
	targets = m.quickTargets(quickFixtureJob(m), 5)
	if len(targets) != 0 || len(m.tunnels) != 12 {
		t.Fatal("repeat changed existing nodes or duplicated the batch")
	}
}

func signedQuickFixture(t *testing.T, root string) (string, string) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fixture CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := x509.ParseCertificate(caDER)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "node.example.com"}, DNSNames: []string{"node.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, issuer, pub, caKey)
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
	return certFile, keyFile
}

func TestQuickSetupReusesCertificateWithoutChangingManagementOrigin(t *testing.T) {
	useSettings(t)
	before := getWebSettings()
	root := t.TempDir()
	cert, key := signedQuickFixture(t, root)
	d := &DomainRegistry{dir: root, values: []RegisteredDomain{{Domain: "node.example.com", CertFile: cert, KeyFile: key}}}
	reused := false
	entry, err := d.quickCertificate("node.example.com", func(string) { reused = true })
	if err != nil || !reused || entry.SourceCert != cert || getWebSettings().PublicHost != before.PublicHost {
		t.Fatal("certificate reuse changed management origin or tried ACME", entry, err)
	}
	spec := quickInboundSpec(entry.Domain, entry)
	tls, err := buildTLS(root, spec)
	if err != nil || tls.SelfSigned || tls.ServerName != "node.example.com" {
		t.Fatal("domain setup used wrong certificate", tls, err)
	}
	normalized, err := normalizeInboundSpec(spec, nil)
	if err != nil || normalized.Flow != "xtls-rprx-vision" {
		t.Fatal("TLS Vision not enabled", normalized, err)
	}
	ib := &nativeInbound{ID: 1, Port: 30001, Protocol: spec.Protocol, Network: spec.Network, Security: spec.Security, TLS: tls}
	client := nativeClient{ID: fixtureUUID, Enable: true, Flow: normalized.Flow}
	u, err := url.Parse(shareLink(ib, client, "192.0.2.1"))
	if err != nil || u.Query().Get("security") != "tls" || u.Query().Get("flow") != "xtls-rprx-vision" || u.Query().Get("sni") != entry.Domain {
		t.Fatal("wrong domain sharing parameters", u, err)
	}
	// A simultaneous domain operation must return promptly, not overlap issuance.
	d.operation.Lock()
	_, err = d.quickCertificate(entry.Domain, func(string) {})
	d.operation.Unlock()
	if err == nil {
		t.Fatal("concurrent certificate operation accepted")
	}
}

func TestQuickSetupRealityNeedsNoCertificate(t *testing.T) {
	spec := quickInboundSpec("", RegisteredDomain{})
	ns, err := normalizeInboundSpec(spec, nil)
	if err != nil || ns.Security != "reality" || ns.Protocol != "vless" || ns.Network != "tcp" || ns.Flow != "xtls-rprx-vision" || spec.CertFile != "" || spec.ServerName != "" {
		t.Fatal("wrong domain-free setup", spec, ns, err)
	}
	n := &Native{store: &nativeStore{NextID: 2, Inbounds: []*nativeInbound{{ID: 1, Enable: true, Protocol: "vless", Network: "tcp", Security: "reality", Remark: spec.Remark, Reality: &realityConfig{PublicKey: "fixture"}, Clients: []nativeClient{{Enable: true, Flow: ns.Flow}}}}}}
	for i := 0; i < 2; i++ {
		id, err := n.quickTemplate(spec, nil)
		if err != nil || id != 1 || len(n.store.Inbounds) != 1 {
			t.Fatal("repeat recreated a matching template", id, err)
		}
	}
}

func TestQuickSetupWithOfficialXrayValidator(t *testing.T) {
	bin := os.Getenv("MIKU_XRAY_TEST_BINARY")
	if bin == "" {
		t.Skip("set MIKU_XRAY_TEST_BINARY to validate both stacks with official Xray")
	}
	root := t.TempDir()
	cert, key := signedQuickFixture(t, root)
	for _, domain := range []string{"", "node.example.com"} {
		spec := quickInboundSpec(domain, RegisteredDomain{CertFile: cert, KeyFile: key})
		spec.Port = 30001
		ns, err := normalizeInboundSpec(spec, nil)
		if err != nil {
			t.Fatal(err)
		}
		ib := &nativeInbound{ID: 1, Port: ns.Port, Protocol: ns.Protocol, Network: ns.Network, Security: ns.Security, Enable: true, Clients: []nativeClient{{ID: fixtureUUID, Email: "fixture", Flow: ns.Flow, Enable: true}}}
		if domain == "" {
			var private, public string
			private, public, err = realityKeys(bin)
			ib.Reality = &realityConfig{Dest: "www.tesla.com:443", ServerNames: []string{"www.tesla.com"}, PrivateKey: private, PublicKey: public, ShortIDs: []string{randomShortID()}, Fingerprint: "chrome"}
		} else {
			ib.TLS, err = buildTLS(root, spec)
		}
		if err != nil {
			t.Fatal(err)
		}
		path, err := writeXrayConfig(root, buildXrayConfig([]*nativeInbound{ib}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if err = verifyXrayConfig(bin, path); err != nil {
			t.Fatalf("official Xray rejected %s: %v", ns.Security, err)
		}
	}
}

func TestQuickSetupValidatesAndDeduplicatesRequests(t *testing.T) {
	m := NewManager(0, t.TempDir())
	m.quickJob = quickFixtureJob(m)
	m.quickJob.SetKind("quick-setup")
	for _, tc := range []struct {
		method, body string
		status       int
	}{{"GET", "", 405}, {"POST", `{`, 400}, {"POST", `{"domain":"https://example.com"}`, 400}, {"POST", `{"domain":"192.0.2.1"}`, 400}, {"POST", `{"domain":"example.com;touch /tmp/unsafe"}`, 400}, {"POST", `{"domain":""} {}`, 400}, {"POST", `{"domain":"","unexpected":1}`, 400}, {"POST", `{"domain":""}`, 202}, {"POST", `{"domain":"Node.Example.Com."}`, 202}} {
		w := httptest.NewRecorder()
		apiQuickSetup(m, nil).ServeHTTP(w, httptest.NewRequest(tc.method, "/api/quick-setup", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if tc.status == 202 && !strings.Contains(w.Body.String(), m.quickJob.ID()) {
			t.Fatal("duplicate request returned a new task")
		}
	}
	m.jobs.Dismiss(m.quickJob.ID())
	if len(m.jobs.Views()) != 1 {
		t.Fatal("running task lost its progress record")
	}
	w := httptest.NewRecorder()
	apiPanelMode(m.workDir, m).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/panel/mode", strings.NewReader(`{"mode":"sing-box"}`)))
	if w.Code != 409 {
		t.Fatal("allowed core change during quick setup", w.Code)
	}
}

func TestQuickSetupCoreFailureCompletesEveryStep(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	n := &Native{dir: m.workDir, store: &nativeStore{}, proc: &xrayProc{bin: "/bin/false", dir: m.workDir, core: "xray"}}
	usePanel(t, n)
	job := quickFixtureJob(m)
	m.runQuickSetup(job, nil, "")
	view := job.View()
	if view.Status != "failed" || view.Done != view.Total || len(m.tunnels) != 0 {
		t.Fatal("failed setup left pending steps or partially created nodes", view)
	}
}

func TestProxyGuideLinksCannotPointAtAnotherExitOrDirectTemplate(t *testing.T) {
	m := NewManager(0, t.TempDir())
	m.tunnels[1] = &Tunnel{Slot: 1, Status: "up", Node: Node{HostName: "jp"}}
	usePanel(t, &Native{store: &nativeStore{Inbounds: []*nativeInbound{
		{ID: 1, Port: 30001, Protocol: "vless", Enable: true, BoundTo: "jp", Clients: []nativeClient{{ID: "correct", Enable: true}}},
		{ID: 2, Port: 30002, Protocol: "vless", Enable: true, BoundTo: "kr", Clients: []nativeClient{{ID: "wrong", Enable: true}}},
		{ID: 3, Port: 30003, Protocol: "vless", Enable: true, Clients: []nativeClient{{ID: "direct", Enable: true}}},
	}}})
	for _, tc := range []struct{ id, status int }{{1, 200}, {2, 404}, {3, 404}} {
		w := httptest.NewRecorder()
		apiExitLinks(m).ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/exits/links?slot=1&id=%d", tc.id), nil))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "wrong@") || strings.Contains(w.Body.String(), "direct@") {
			t.Fatal("proxy guide could leak another entry", w.Code, w.Body.String())
		}
	}
}

// Optional browser fixture: render the exact embedded HTML without starting
// privileged networking or the production server.
func TestPanelBrowserFixture(t *testing.T) {
	output := os.Getenv("MIKU_UI_OUTPUT")
	if output == "" {
		t.Skip("set MIKU_UI_OUTPUT for browser verification")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "index.html"), []byte(indexHTML), 0600); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]string{"version": version})
	if err := os.WriteFile(filepath.Join(output, "fixture.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
}
