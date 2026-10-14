package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestExitOrderSurvivesSlotReuseAndRestart(t *testing.T) {
	m := NewManager(0, t.TempDir())
	m.tunnels[2] = &Tunnel{Slot: 2, Order: 2, Status: "failed", Node: Node{HostName: "old"}, Cred: SocksCred{User: "u", Pass: "p"}}
	first := m.reserveOrders(2)
	m.tunnels[1] = &Tunnel{Slot: 1, Order: first, Status: "failed", Node: Node{HostName: "new"}, Cred: SocksCred{User: "u", Pass: "p"}}
	if got := m.Tunnels(); got[0].Node.HostName != "old" || got[1].Node.HostName != "new" {
		t.Fatal("slot reuse moved new exit to top")
	}
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(statePath(m.workDir))
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.NextOrder != 4 || state.Tunnels[1].Order != 3 {
		t.Fatal("creation order not saved", state)
	}
}

func TestJobsFollowClickOrder(t *testing.T) {
	var store JobStore
	a := store.New("first", nil)
	b := store.New("second", nil)
	views := store.Views()
	if views[0].ID != a.ID() || views[1].ID != b.ID() {
		t.Fatal("new job inserted before old job")
	}
}

func TestDisconnectPreservesOtherUsersAndTemplate(t *testing.T) {
	clone := &nativeInbound{ID: 1, Enable: true, BoundTo: "jp", Clients: []nativeClient{{Email: "clone", ID: "keep-uuid", Enable: true}}}
	shared := &nativeInbound{ID: 2, Enable: true, Clients: []nativeClient{{Email: "jp", Enable: true, BoundTo: "jp"}, {Email: "kr", Enable: true, BoundTo: "kr"}}}
	template := &nativeInbound{ID: 3, Enable: true, Clients: []nativeClient{{Email: "direct", Enable: true}}}
	store := &nativeStore{Inbounds: []*nativeInbound{clone, shared, template}}
	if !disableNativeExit(store, map[string]bool{"jp": true}) {
		t.Fatal("exit not disabled")
	}
	if clone.Enable || clone.Clients[0].Enable || clone.Clients[0].ID != "keep-uuid" {
		t.Fatal("clone still active or identity lost")
	}
	if !shared.Enable || shared.Clients[0].Enable || !shared.Clients[1].Enable || !template.Enable {
		t.Fatal("other exits or template affected")
	}
}

func TestCommunityPriorityKeepsTestedMetadataAndOfficialIsLast(t *testing.T) {
	nodes := mergeSourceNodes(nil, []Node{{HostName: "same", IP: "198.51.100.1", Priority: 1, Pool: &PoolProfile{}}})
	nodes = mergeSourceNodes(nodes, []Node{{HostName: "same", IP: "198.51.100.1", Priority: 2, Config: "client"}})
	nodes = mergeSourceNodes(nodes, []Node{{HostName: "same", IP: "198.51.100.1", Priority: 9, Config: "older"}})
	if nodes[0].Config != "client" || nodes[0].Priority != 1 {
		t.Fatal("official displaced community or lost test rank", nodes)
	}
	sources := configuredSources()
	if sources[len(sources)-1] != vpngateAPI {
		t.Fatal("official source not last")
	}
}

func TestStoppedBindingsStayOutOfEntranceAreaWhileSharedExitRemains(t *testing.T) {
	store := &nativeStore{Inbounds: []*nativeInbound{
		{ID: 1, Port: 30001, Protocol: "vless", BoundTo: "jp", Enable: false, Clients: []nativeClient{{Enable: false}}},
		{ID: 2, Port: 30002, Protocol: "vless", BoundTo: "jp", Enable: true, Clients: []nativeClient{{Email: "other", Enable: true, BoundTo: "kr"}}},
		{ID: 3, Port: 30003, Protocol: "vless", Enable: true},
		{ID: 4, Port: 30004, Protocol: "vless", BoundTo: "missing", Enable: true},
	}}
	panelState.mu.Lock()
	previous := panelState.current
	panelState.current = &Native{store: store, proc: &xrayProc{core: "xray"}}
	panelState.mu.Unlock()
	invalidateInbounds()
	t.Cleanup(func() {
		panelState.mu.Lock()
		panelState.current = previous
		panelState.mu.Unlock()
		invalidateInbounds()
	})
	m := NewManager(0, t.TempDir())
	m.tunnels[1] = &Tunnel{Slot: 1, Node: Node{HostName: "kr"}, Status: "up"}
	view := m.ExitsOf()
	if len(view.Direct) != 1 || view.Direct[0].ID != 3 {
		t.Fatal("stopped or missing exit promoted to entrance", view.Direct)
	}
	if len(view.Exits) != 1 || len(view.Exits[0].Inbounds) != 1 || view.Exits[0].Inbounds[0].ID != 2 {
		t.Fatal("shared entrance missing from remaining exit", view.Exits)
	}
}
