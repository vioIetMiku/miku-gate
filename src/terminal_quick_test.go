package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func terminalQuickFixture(t *testing.T) (*Manager, *Native) {
	t.Helper()
	m := NewManager(0, t.TempDir())
	template := &nativeInbound{ID: 1, Port: 30000, Enable: true, Protocol: "vless", Network: "tcp", Security: "reality", Remark: "Miku 一键 · Reality",
		Reality: &realityConfig{PublicKey: "fixture-public", ServerNames: []string{"www.amd.com"}, ShortIDs: []string{"abcd"}, Fingerprint: "chrome"},
		Clients: []nativeClient{{ID: fixtureUUID, Enable: true, Flow: "xtls-rprx-vision"}}}
	n := &Native{store: &nativeStore{NextID: 13, Inbounds: []*nativeInbound{template}}}
	slot := 0
	for _, r := range quickRegions {
		for i := 0; i < r.Count; i++ {
			slot++
			host := fmt.Sprintf("%s-%d", r.Code, i)
			m.tunnels[slot] = &Tunnel{Slot: slot, TemplateID: 1, Status: "up", EntryReady: true, PreferredRegion: r.Code, Node: Node{HostName: host, CountryCode: r.Code}}
			inbound := *template
			inbound.ID, inbound.Port, inbound.Remark = slot+1, 30000+slot, host
			inbound.BoundTo, inbound.AutoTemplate = sanitizeTag(host), 1
			n.store.Inbounds = append(n.store.Inbounds, &inbound)
		}
	}
	return m, n
}

func TestTerminalQuickReusesElevenAfterRestartAndIgnoresUnrelatedNodes(t *testing.T) {
	m, n := terminalQuickFixture(t)
	before := m.terminalQuickView(n, "192.0.2.1")
	if !before.Complete || before.Ready != 11 || len(before.Links) != 11 {
		t.Fatal("wrong managed family", before.Assigned, before.Ready)
	}
	root := t.TempDir()
	if err := n.store.save(root); err != nil {
		t.Fatal(err)
	}
	store, err := loadNativeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// Offline exits preserve their links, without allocating a replacement.
	m.tunnels[1].Status = "failed"
	unrelated := *store.Inbounds[1]
	unrelated.ID, unrelated.Port, unrelated.AutoTemplate = 99, 40000, 99
	store.Inbounds = append(store.Inbounds, &unrelated)
	after := m.terminalQuickView(&Native{store: store}, "192.0.2.1")
	if !after.Complete || after.Ready != 10 || !reflect.DeepEqual(before.Links, after.Links) {
		t.Fatal("restart replaced identities or included unrelated entries")
	}
}

func TestTerminalQuickPostOnCompleteFamilyNeverStartsAnotherJob(t *testing.T) {
	m, n := terminalQuickFixture(t)
	panelState.mu.Lock()
	previous := panelState.current
	panelState.current = n
	panelState.mu.Unlock()
	defer func() { panelState.mu.Lock(); panelState.current = previous; panelState.mu.Unlock() }()
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		apiTerminalQuick(m, nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/quick-setup/terminal", nil))
		var result terminalQuickView
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Existing || len(result.Links) != 11 || m.quickJob != nil {
			t.Fatal("repeated quick command created a task or lost nodes", w.Code)
		}
	}
}

func TestRealityWebSocketValidationDependsOnCore(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess"} {
		spec := NewInboundSpec{Protocol: protocol, Network: "ws", Security: "reality", Port: 30001}
		if _, err := normalizeInboundSpecForCore(spec, nil, "sing-box"); err != nil {
			t.Fatal(protocol, err)
		}
		if _, err := normalizeInboundSpecForCore(spec, nil, "xray"); err == nil {
			t.Fatal("Xray accepted WebSocket Reality")
		}
	}
}

// CI starts real pinned sing-box servers and clients using these generated
// configs, and imports each client through the actual sharing-link parser.
func TestTerminalProtocolFixtures(t *testing.T) {
	root := os.Getenv("MIKU_TERMINAL_CORE_FIXTURE")
	if root == "" {
		t.Skip("set MIKU_TERMINAL_CORE_FIXTURE for official core roundtrip")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ protocol, network string }{{"vless", "ws"}, {"vmess", "ws"}, {"trojan", "tcp"}} {
		spec, err := normalizeInboundSpecForCore(NewInboundSpec{Protocol: item.protocol, Network: item.network, Security: "reality", Port: 30001}, nil, "sing-box")
		if err != nil {
			t.Fatal(err)
		}
		ib := &nativeInbound{ID: 1, Enable: true, Port: spec.Port, Protocol: spec.Protocol, Network: spec.Network, Path: spec.Path, Security: spec.Security, Remark: spec.Protocol,
			Reality: &realityConfig{Dest: "127.0.0.1:443", ServerNames: []string{"localhost"}, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortIDs: []string{"abcdef12"}, Fingerprint: "chrome"},
			Clients: []nativeClient{{ID: newUUID(), Password: randomHex(16), Email: "fixture", Enable: true}}}
		server, err := buildSingBoxConfig([]*nativeInbound{ib}, nil)
		if err != nil {
			t.Fatal(err)
		}
		node, err := parseSubscriptionNode(shareLink(ib, ib.Clients[0], "127.0.0.1"))
		if err != nil {
			t.Fatal(err)
		}
		client, err := node.singBoxProxy()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{"server": server, "client": client})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, item.protocol+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
