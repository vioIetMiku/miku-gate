package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func localLinkFixture(t *testing.T) (*Manager, *Native, *nativeInbound) {
	t.Helper()
	useSettings(t)
	webSettingsMu.Lock()
	webSettingsCur.PublicHost = "198.51.100.10"
	webSettingsMu.Unlock()
	publicIPMu.Lock()
	previous := publicIPOverride
	publicIPOverride = "198.51.100.10"
	publicIPMu.Unlock()
	t.Cleanup(func() { setPublicIPOverride(previous) })
	ib := &nativeInbound{ID: 1, Port: 30001, Protocol: "vless", Enable: true, BoundTo: "jp", Clients: []nativeClient{{ID: fixtureUUID, Email: "jp-user", Enable: true}}}
	n := &Native{store: &nativeStore{NextID: 2, Inbounds: []*nativeInbound{ib}}, proc: &xrayProc{core: "xray"}}
	usePanel(t, n)
	m := NewManager(0, t.TempDir())
	m.tunnels[1] = &Tunnel{Slot: 1, Order: 1, Status: "up", Port: 31001, ExitIP: "203.0.113.7", Node: Node{HostName: "jp", CountryCode: "JP"}, Cred: SocksCred{User: "u@local", Pass: "p:local"}}
	return m, n, ib
}

func TestDisabledLegacyPortsAndOverriddenClientsStayOutOfExit(t *testing.T) {
	m, n, ib := localLinkFixture(t)
	old := *ib
	old.ID, old.Port, old.Enable = 2, 30002, false
	shared := *ib
	shared.ID, shared.Port = 3, 30003
	shared.Clients = []nativeClient{{ID: "korean", Enable: true, BoundTo: "kr"}}
	n.store.Inbounds = append(n.store.Inbounds, &old, &shared)
	m.tunnels[2] = &Tunnel{Slot: 2, Order: 2, Status: "up", Node: Node{HostName: "kr"}}
	view := m.ExitsOf()
	if len(view.Exits[0].Inbounds) != 1 || view.Exits[0].Inbounds[0].ID != 1 || len(view.Exits[1].Inbounds) != 1 || view.Exits[1].Inbounds[0].ID != 3 {
		t.Fatalf("disabled or overridden listener leaked into exit: %+v", view.Exits)
	}
}

func TestRepeatNativeCloneReusesExistingPort(t *testing.T) {
	m, n, ib := localLinkFixture(t)
	ports, err := n.CloneToTunnels(ib.ID, []string{"jp", "jp"}, m.Tunnels())
	if err != nil || len(ports) != 1 || ports[0] != ib.Port || len(n.store.Inbounds) != 1 {
		t.Fatal("repeat clone allocated another listener", ports, err)
	}
}

func TestExitCopyFiltersSharedListenerAndUsesVPSServer(t *testing.T) {
	m, n, ib := localLinkFixture(t)
	ib.Clients = append(ib.Clients, nativeClient{ID: "kr-credential", Enable: true, BoundTo: "kr"}, nativeClient{ID: "disabled", Enable: false})
	m.tunnels[2] = &Tunnel{Slot: 2, Status: "up", Node: Node{HostName: "kr"}}
	stale := *ib
	stale.ID, stale.Port, stale.Enable = 2, 30002, false
	n.store.Inbounds = append(n.store.Inbounds, &stale)
	links, err := m.exitLinks(1, 1, "198.51.100.10")
	if err != nil || len(links) != 1 || !strings.Contains(links[0], fixtureUUID+"@198.51.100.10:30001") || strings.Contains(links[0], "203.0.113.7") {
		t.Fatal("wrong exit credentials/address copied", links, err)
	}
	links, err = m.exitLinks(2, 1, "198.51.100.10")
	if err != nil || len(links) != 1 || !strings.Contains(links[0], "kr-credential") {
		t.Fatal("shared entrance copied wrong client", links, err)
	}
	for _, query := range []string{"slot=1&id=2", "slot=1&id=999"} {
		rec := httptest.NewRecorder()
		apiExitLinks(m)(rec, httptest.NewRequest("GET", "/api/exits/links?"+query, nil))
		if rec.Code != 404 {
			t.Fatal("inactive listener exported", rec.Code, rec.Body.String())
		}
	}
	m.tunnels[1].Status = "starting"
	if _, err := m.exitLinks(1, 1, "198.51.100.10"); err == nil {
		t.Fatal("unready exit exported")
	}
}

func TestPastedLocalLinksRecognizeWithoutCreatingAndExpireAfterReset(t *testing.T) {
	m, n, ib := localLinkFixture(t)
	link := shareLink(ib, ib.Clients[0], "198.51.100.10")
	added, bookmarks, err := m.importLinks(link+"\n"+strings.Split(link, "#")[0]+"#Renamed", "")
	if err != nil || added != 1 || len(bookmarks) != 1 || bookmarks[0].InboundID != ib.ID || len(n.store.Inbounds) != 1 {
		t.Fatal("local recognition duplicated listener/bookmark", bookmarks, err)
	}
	for _, bad := range []string{strings.Replace(link, "198.51.100.10", "remote.example.com", 1), strings.Replace(link, fixtureUUID, "00000000-0000-4000-8000-000000000002", 1), strings.Replace(link, "30001", "30002", 1), strings.Replace(link, "type=tcp", "type=ws", 1)} {
		if _, _, err := m.importLinks(bad, ""); err == nil {
			t.Fatal("foreign or altered link accepted", bad)
		}
	}
	links, err := subLinks(m, "198.51.100.10", true)
	if err != nil || len(links) != 1 {
		t.Fatal("bookmark duplicated subscription", links, err)
	}
	ib.Clients[0].ID = "replacement"
	if len(m.verifiedImportedLinks()) != 0 {
		t.Fatal("stale credential still recognized")
	}
	links, err = subLinks(m, "198.51.100.10", true)
	if err != nil || len(links) != 1 || strings.Contains(links[0], fixtureUUID) {
		t.Fatal("old bookmark bypassed reset", links, err)
	}
	ib.Enable = false
	links, err = subLinks(m, "198.51.100.10", true)
	if err != nil || len(links) != 0 {
		t.Fatal("disabled bookmark still exported", links, err)
	}
}

func TestPastedProxyVerifiesVPSEndpointAndLegacyRemoteImportIsExcluded(t *testing.T) {
	m, _, _ := localLinkFixture(t)
	for _, scheme := range []string{"socks5", "http"} {
		link := scheme + "://u%40local:p%3Alocal@198.51.100.10:31001#Local"
		_, items, err := m.importLinks(link, "")
		if err != nil || len(items) == 0 || items[len(items)-1].Slot != 1 {
			t.Fatal("local proxy not recognized", items, err)
		}
		if _, _, err := m.importLinks(strings.Replace(link, "198.51.100.10", "203.0.113.7", 1), ""); err == nil {
			t.Fatal("exit IP mistaken for VPS address")
		}
	}
	remote, _ := parseImportedLink("vless://" + fixtureUUID + "@remote.example.com:30001?type=tcp&security=none")
	blob, _ := json.Marshal([]ImportedLink{remote})
	if err := os.WriteFile(filepath.Join(m.workDir, "imported-links.json"), blob, 0600); err != nil {
		t.Fatal(err)
	}
	links, err := subLinks(m, "198.51.100.10", true)
	if err != nil || len(links) != 1 || strings.Contains(links[0], "remote.example.com") {
		t.Fatal("legacy foreign link escaped into subscription", links, err)
	}
}

func TestOwnershipUsesOrganizationTypesOrProviderInformation(t *testing.T) {
	cases := []struct {
		a, c            string
		hosting, mobile *bool
		want            string
	}{
		{"ISP", "isp", nil, nil, "residential"}, {"bus", "ISP", nil, nil, "residential"}, {"isp", "business", nil, nil, "residential"},
		{"business", "business", nil, nil, "residential"}, {"isp", "", nil, nil, "residential"}, {"", "", boolPtr(false), boolPtr(false), "residential"},
		{"hosting", "isp", nil, nil, "datacenter"}, {"isp", "isp", boolPtr(true), nil, "datacenter"}, {"isp", "isp", nil, boolPtr(true), "mobile"},
	}
	for _, tc := range cases {
		info := IPInfo{ASNType: tc.a, CompanyType: tc.c, Hosting: tc.hosting, Mobile: tc.mobile, Organization: "Example ISP"}
		classifyIPInfo(&info)
		if info.IPType != tc.want || info.TypeReason == "" {
			t.Fatal("wrong classification", tc, info)
		}
	}
}

func TestInspectorReadsBothTypesAndRefreshesWhenKeyChanges(t *testing.T) {
	useSettings(t)
	webSettingsMu.Lock()
	webSettingsCur.IPInfoKey = "fixture-secret"
	webSettingsMu.Unlock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body["key"] != "fixture-secret" || r.URL.RawQuery != "" {
			t.Error("key was not kept in POST body")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ip":"8.8.8.8","asn":{"asn":123,"org":"Example","type":"isp"},"company":{"name":"Example Customer","type":"business"},"location":{"country":"Japan","city":"Tokyo"},"is_datacenter":false,"is_mobile":false}`))
	}))
	defer server.Close()
	s := &IPInspector{cache: map[string]IPInfo{}, pending: map[string]bool{}, queue: make(chan string, 2), blocked: map[string]time.Time{}, dir: t.TempDir(), client: server.Client(), isAPI: server.URL}
	s.lookup("8.8.8.8")
	value := s.get("8.8.8.8")
	if value == nil || value.IPType != "residential" || value.ASNType != "isp" || value.CompanyType != "business" || value.Source != "ipapi.is" {
		t.Fatal("two type data lost", value)
	}
	s.typeKey = "fixture-secret"
	webSettingsMu.Lock()
	webSettingsCur.IPInfoKey = "new-secret"
	webSettingsMu.Unlock()
	s.enqueue("8.8.8.8")
	if !s.pending["8.8.8.8"] {
		t.Fatal("new key did not refresh cached classification")
	}
}
