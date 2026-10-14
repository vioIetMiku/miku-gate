package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const fixtureUUID = "00000000-0000-4000-8000-000000000001"

func TestParallelRetriesCannotClaimSameEndpoint(t *testing.T) {
	m := NewManager(0, t.TempDir())
	for i := 1; i <= 8; i++ {
		m.tunnels[i] = &Tunnel{Slot: i, Status: "starting", PreferredRegion: "JP", Node: Node{HostName: string(rune('a' + i)), CountryCode: "JP"}}
	}
	candidate := Node{HostName: "available", IP: "198.51.100.8", CountryCode: "JP"}
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for _, tunnel := range m.Tunnels() {
		wg.Add(1)
		go func(t *Tunnel) { defer wg.Done(); results <- m.claimCandidate(t, candidate) }(tunnel)
	}
	wg.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("same endpoint acquired by %d exits", winners)
	}
	for _, tunnel := range m.Tunnels() {
		if m.claimCandidate(tunnel, Node{HostName: "foreign", CountryCode: "KR"}) {
			t.Fatal("foreign candidate accepted")
		}
	}
}

func TestActualExitIPAlsoDeduplicated(t *testing.T) {
	m := NewManager(0, t.TempDir())
	a := &Tunnel{Slot: 1, Status: "starting", ExitIP: "198.51.100.8"}
	b := &Tunnel{Slot: 2, Status: "starting", ExitIP: a.ExitIP}
	m.tunnels[1], m.tunnels[2] = a, b
	if !m.acceptExitIP(a) || m.acceptExitIP(b) {
		t.Fatal("duplicate actual IP accepted")
	}
}

func TestNativeEntryRepairsMissingDuplicateAndChangedHost(t *testing.T) {
	client := nativeClient{ID: fixtureUUID, Email: "fixture", Enable: true}
	tpl := &nativeInbound{ID: 1, Port: 30000, Protocol: "vless", Enable: true, Clients: []nativeClient{client}}
	store := &nativeStore{NextID: 2, Inbounds: []*nativeInbound{tpl}}
	exit := &Tunnel{Slot: 1, Order: 9, Node: Node{HostName: "jp-a", CountryCode: "JP"}, Status: "up"}
	allocations := 0
	allocate := func(map[int]bool) (int, error) { allocations++; return 30001, nil }
	changed, err := reconcileNativeEntry(store, tpl, exit, allocate)
	if err != nil || !changed || len(store.Inbounds) != 2 {
		t.Fatal("missing entry not created", err)
	}
	created := store.Inbounds[1]
	for i := 0; i < 3; i++ {
		if changed, err := reconcileNativeEntry(store, tpl, exit, allocate); changed || err != nil {
			t.Fatal("reconciliation is not idempotent", err)
		}
	}
	if allocations != 1 {
		t.Fatal("allocated duplicate ports")
	}
	duplicate := *created
	duplicate.ID, duplicate.Port, duplicate.Remark = 3, 30002, "🇯🇵 日本 2"
	duplicate.AutoExitOrder = 0
	store.Inbounds = append(store.Inbounds, &duplicate)
	if _, err := reconcileNativeEntry(store, tpl, exit, allocate); err != nil || duplicate.Enable {
		t.Fatal("legacy duplicate remained active", err)
	}
	exit.Node.HostName = "jp-b"
	if _, err := reconcileNativeEntry(store, tpl, exit, allocate); err != nil || created.BoundTo != "jp-b" || created.Port != 30001 || created.Clients[0].ID != fixtureUUID {
		t.Fatal("failover lost entry identity", err)
	}
}

func TestSourceReorderChangesSelectionAndIsPersisted(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = []Node{{HostName: "pool", CountryCode: "JP", Source: gatePoolIndex, Priority: 10}, {HostName: "official", CountryCode: "JP", Source: vpngateAPI, Priority: 60}}
	body, _ := json.Marshal(map[string]any{"order": []string{vpngateAPI, gatePoolIndex}})
	rec := httptest.NewRecorder()
	apiSourceOrder(m)(rec, httptest.NewRequest(http.MethodPost, "/api/sources/order", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if m.nodes[0].HostName != "official" || getWebSettings().VPNGateSourceOrder[0] != vpngateAPI {
		t.Fatal("source preference is only cosmetic")
	}
	picks, err := m.pickNodes("JP", 1, nil)
	if err != nil || picks[0].HostName != "official" {
		t.Fatal("selection ignored preferred library", picks, err)
	}
}

func TestSubscriptionFormatsPreserveCredentialsAndTransport(t *testing.T) {
	reality := "vless://" + fixtureUUID + "@198.51.100.8:443?type=tcp&security=reality&sni=example.com&pbk=fixtureKey&sid=0123&fp=chrome&flow=xtls-rprx-vision#Japan"
	vmess := subscriptionNode{Protocol: "vmess", Name: "WebSocket", Server: "ws.example.com", Port: 443, UUID: fixtureUUID, Cipher: "auto", Network: "ws", Security: "tls", ServerName: "ws.example.com", Host: "ws.example.com", Path: "/socket"}.vmessLink()
	links := []string{reality, vmess, "socks5://a%40b:p%3Aa@198.51.100.9:1080#Proxy"}
	body, content, err := formatSubscription(links, "clash")
	if err != nil || !strings.Contains(content, "yaml") {
		t.Fatal(err, content)
	}
	proxies := []map[string]any{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "  - {") && !strings.Contains(line, "\"proxies\":") {
			var value map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "  - ")), &value) != nil {
				t.Fatal("invalid YAML inline JSON")
			}
			proxies = append(proxies, value)
		}
	}
	if len(proxies) != 3 || proxies[0]["flow"] != "xtls-rprx-vision" || proxies[0]["reality-opts"].(map[string]any)["public-key"] != "fixtureKey" || proxies[1]["ws-opts"].(map[string]any)["path"] != "/socket" || proxies[2]["username"] != "a@b" || proxies[2]["password"] != "p:a" {
		t.Fatal("Clash credentials/transport lost", proxies)
	}
	body, content, err = formatSubscription(links, "singbox")
	var config map[string]any
	if err != nil || !strings.Contains(content, "json") || json.Unmarshal([]byte(body), &config) != nil {
		t.Fatal(err, body)
	}
	out := config["outbounds"].([]any)
	if len(out) != 4 || out[1].(map[string]any)["uuid"] != fixtureUUID || out[2].(map[string]any)["transport"].(map[string]any)["type"] != "ws" || out[3].(map[string]any)["type"] != "socks" {
		t.Fatal("sing-box format incorrect", out)
	}
	body, _, err = formatSubscription(links, "v2ray")
	decoded, decodeErr := base64.StdEncoding.DecodeString(body)
	if err != nil || decodeErr != nil || !strings.Contains(string(decoded), vmess) {
		t.Fatal("V2Ray base64 incomplete", err)
	}
	if _, _, err := formatSubscription([]string{strings.Replace(reality, "type=tcp", "type=xhttp", 1)}, "singbox"); err == nil {
		t.Fatal("unsupported transport silently weakened")
	}
}

func TestImportConfirmationIsAtomicPersistentAndDeduplicated(t *testing.T) {
	useSettings(t)
	webSettingsMu.Lock()
	webSettingsCur.PublicHost = "198.51.100.8"
	webSettingsMu.Unlock()
	usePanel(t, &Native{store: &nativeStore{Inbounds: []*nativeInbound{{ID: 1, Port: 443, Protocol: "vless", Enable: true, Security: "tls", TLS: &tlsConfig{ServerName: "example.com"}, Clients: []nativeClient{{ID: fixtureUUID, Enable: true}}}}}, proc: &xrayProc{core: "xray"}})
	m := NewManager(0, t.TempDir())
	link := "vless://" + fixtureUUID + "@198.51.100.8:443?security=tls&sni=example.com#One"
	added, items, err := m.importLinks(link+"\n"+strings.Replace(link, "#One", "#Another", 1), "")
	if err != nil || added != 1 || len(items) != 1 {
		t.Fatal("duplicate import", added, err)
	}
	if _, _, err := m.importLinks(link+"\ninvalid://abc", ""); err == nil {
		t.Fatal("partial bad import accepted")
	}
	stored, err := m.readImportedLinks()
	if err != nil || len(stored) != 1 {
		t.Fatal("import not persisted", err)
	}
	_, items, err = m.importLinks("", items[0].ID)
	if err != nil || len(items) != 0 {
		t.Fatal("remove failed", err)
	}
}

func TestNativeSubscriptionFiltersClientsAndDoesNotDuplicateSharedEntry(t *testing.T) {
	useSettings(t)
	clients := []nativeClient{{ID: fixtureUUID, Email: "jp", Enable: true, BoundTo: "jp"}, {ID: "different", Email: "kr", Enable: true, BoundTo: "offline"}, {ID: "disabled", Enable: false, BoundTo: "jp"}}
	panel := &Native{store: &nativeStore{Inbounds: []*nativeInbound{{ID: 1, Port: 30001, Protocol: "vless", Enable: true, Clients: clients}}}, proc: &xrayProc{core: "xray"}}
	usePanel(t, panel)
	m := NewManager(0, t.TempDir())
	m.tunnels[1] = &Tunnel{Slot: 1, Status: "up", Node: Node{HostName: "jp"}}
	links, err := subLinks(m, "198.51.100.1", true)
	if err != nil || len(links) != 1 || !strings.Contains(links[0], fixtureUUID) {
		t.Fatal("inactive or duplicate client in subscription", links, err)
	}
}
