package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func latencyFixture() *LatencyInspector {
	return &LatencyInspector{cache: map[string]GateLatency{}, pending: map[string]bool{}, queue: make(chan string, 32), profiles: map[string]Node{}, icmpBlocked: map[string]time.Time{}, tunnelSamples: map[string]GateLatency{}}
}

func TestDev32OnlyConfiguredEndpointsAndProtocolSensitiveCache(t *testing.T) {
	node := Node{IP: "198.51.100.8", Config: "remote 'vpn.example.org'\nproto tcp-client\nrport 2443\nremote 203.0.113.8 1194 udp\nremote 127.0.0.1 443 tcp\n<ca>\nremote 203.0.113.9 80 tcp\n</ca>\nremote 203.0.113.8 1194 udp\n"}
	want := []gateEndpoint{{"198.51.100.8", 2443, "tcp-client"}, {"203.0.113.8", 1194, "udp"}}
	if got := nodeProbeEndpoints(node); !reflect.DeepEqual(got, want) {
		t.Fatalf("wrong configured endpoints: %+v", got)
	}
	a := Node{IP: node.IP, Config: "proto tcp-client\nremote 198.51.100.8 2443"}
	b, c := a, a
	b.Config = strings.ReplaceAll(a.Config, "2443", "443")
	c.Config = strings.ReplaceAll(a.Config, "tcp-client", "udp")
	if latencyKey(a) == latencyKey(b) || latencyKey(a) == latencyKey(c) {
		t.Fatal("latency from another port or protocol can be reused")
	}
	if len(nodeProbeEndpoints(Node{IP: node.IP})) != 0 {
		t.Fatal("missing config invented a TCP port")
	}
}

func TestDev32BlockedICMPFallsBackToRealTCPAndCoolsDown(t *testing.T) {
	s := latencyFixture()
	icmpCalls, tcpCalls := 0, 0
	s.probe = func(string) (float64, error) { icmpCalls++; return 0, errors.New("ICMP blocked") }
	s.tcpProbe = func(ctx context.Context, endpoint gateEndpoint) (float64, error) {
		tcpCalls++
		if endpoint.Port != 2443 || endpoint.IP != "198.51.100.8" || !strings.HasPrefix(endpoint.Proto, "tcp") {
			t.Fatal("unconfigured endpoint probed", endpoint)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("TCP probe has no deadline")
		}
		return 12.25, nil
	}
	node := Node{IP: "198.51.100.8", Config: "proto tcp-client\nremote 198.51.100.8 2443"}
	s.get(node)
	s.measure(latencyKey(node))
	got := s.get(node)
	if got.MS == nil || *got.MS != 12.25 || got.Method != "tcp" || got.Port != 2443 || got.Error != "" {
		t.Fatalf("configured TCP did not replace blocked ICMP: %+v", got)
	}
	s.measure(latencyKey(node))
	if icmpCalls != 1 || tcpCalls != 2 {
		t.Fatal("blocked ICMP was repeatedly retried", icmpCalls, tcpCalls)
	}
	s.icmpBlocked[node.IP] = time.Now().Add(-time.Second)
	s.probe = func(string) (float64, error) { return .125, nil }
	s.measure(latencyKey(node))
	if got = s.get(node); got.Method != "icmp" || got.MS == nil || *got.MS != .125 {
		t.Fatal("ICMP recovery was ignored", got)
	}
}

func TestDev32UDPUsesFreshTunnelReferenceWithoutInventingTCP(t *testing.T) {
	s := latencyFixture()
	s.probe = func(string) (float64, error) { return 0, errors.New("blocked") }
	s.tcpProbe = func(context.Context, gateEndpoint) (float64, error) {
		t.Fatal("UDP treated as a TCP service")
		return 0, nil
	}
	node := Node{IP: "198.51.100.8", Config: "proto udp\nremote 198.51.100.8 1194"}
	s.get(node)
	s.measure(latencyKey(node))
	if got := s.get(node); got.MS != nil || got.Error == "" {
		t.Fatal("failed probes turned into zero latency", got)
	}
	s.recordTunnelSample(node, 140.75, "https://api.ipify.org")
	if got := s.get(node); got.MS == nil || *got.MS != 140.75 || got.Method != "tunnel_tcp" || got.Target != "https://api.ipify.org" {
		t.Fatal("fresh tunnel fallback unavailable", got)
	}
	sample := s.tunnelSamples[latencyKey(node)]
	sample.Checked = time.Now().Add(-46 * time.Second)
	s.tunnelSamples[latencyKey(node)] = sample
	if got := s.get(node); got.MS != nil {
		t.Fatal("expired tunnel sample still displayed", got)
	}
	delete(s.tunnelSamples, latencyKey(node))
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		s.recordTunnelSample(node, bad, "https://api.ipify.org")
	}
	if len(s.tunnelSamples) != 0 {
		t.Fatal("invalid timing cached")
	}
}

func TestDev32TunnelTimingSubtractsDNSAndRejectsMalformedValues(t *testing.T) {
	body, ms, err := parseTunnelTiming([]byte("198.51.100.8\nMIKU_CONNECT:0.140500:0.020250\n"))
	if err != nil || body != "198.51.100.8" || math.Abs(ms-120.25) > .00001 {
		t.Fatal("DNS cost not removed from connection time", body, ms, err)
	}
	for _, value := range []string{"0.1:0.2", "NaN:0", "0:NaN", "+Inf:0", "0:-1", "-1:0", "0.1:0:0", "oops", ""} {
		if _, _, err := parseTunnelTiming([]byte("198.51.100.8\nMIKU_CONNECT:" + value)); err == nil {
			t.Fatal("malformed time accepted", value)
		}
	}
	if _, _, err := parseTunnelTiming([]byte("198.51.100.8")); err == nil {
		t.Fatal("missing curl timing accepted")
	}
}

func TestDev32HealthFallbackAndExitIPMismatch(t *testing.T) {
	task := healthTask{ExitIP: "198.51.100.8", Namespace: "fixture-ns"}
	calls := 0
	result := probeTunnelHealthWith(task, func(ns, url string) (string, float64, error) {
		calls++
		if ns != task.Namespace {
			t.Fatal("probe escaped its tunnel namespace")
		}
		if url == "https://api.ipify.org" {
			return "", 0, errors.New("site unavailable")
		}
		return "198.51.100.8\n日本 大阪府 大阪市\nAS17511\nOPTAGE Inc.", 111.25, nil
	})
	if !result.Healthy || calls != 2 || result.Geo == nil || result.Geo.CountryCode != "JP" || result.MS != 111.25 {
		t.Fatal("second health source did not recover", result, calls)
	}
	calls = 0
	result = probeTunnelHealthWith(task, func(string, string) (string, float64, error) {
		calls++
		return "198.51.100.9", 3, nil
	})
	if result.Healthy || calls != 1 {
		t.Fatal("wrong actual exit IP masked by another source", result, calls)
	}
	result = probeTunnelHealthWith(task, func(string, string) (string, float64, error) { return "<html>error</html>", 3, nil })
	if result.Healthy {
		t.Fatal("error page accepted as healthy")
	}
}

func TestDev32StaleHealthCannotReconnectChangedStoppedOrReusedSlot(t *testing.T) {
	m := NewManager(0, t.TempDir())
	exit := &Tunnel{Slot: 1, Status: "up", ExitIP: "198.51.100.8", Node: Node{IP: "203.0.113.8", Config: "proto tcp-client\nremote 203.0.113.8 443"}}
	m.tunnels[1] = exit
	task, ok := m.healthSnapshot(exit)
	if !ok || !m.healthResultCurrent(healthResult{Task: task}) {
		t.Fatal("fresh health snapshot rejected")
	}
	exit.Node.Config = strings.ReplaceAll(exit.Node.Config, "443", "2443")
	if m.healthResultCurrent(healthResult{Task: task}) {
		t.Fatal("old port health accepted after endpoint change")
	}
	m.reconnectExpected(exit, "old", &task)
	if exit.Status != "up" || exit.ExitIP != task.ExitIP {
		t.Fatal("stale health restarted the changed exit")
	}
	exit.Node = task.Node
	exit.Status = "stopped"
	m.reconnectExpected(exit, "old", &task)
	if exit.Status != "stopped" {
		t.Fatal("stopped exit restarted by health result")
	}
	exit.Status = "up"
	m.tunnels[1] = &Tunnel{Slot: 1, Status: "up", Node: task.Node, ExitIP: task.ExitIP}
	if m.healthResultCurrent(healthResult{Task: task}) {
		t.Fatal("health result attached to a reused slot")
	}
	m.reconnectExpected(exit, "old", &task)
	if exit.Status != "up" {
		t.Fatal("removed tunnel restarted")
	}
}

func TestDev32PreflightRanksASNPlaceAndReachabilityAndRejectsForeignCountry(t *testing.T) {
	m := NewManager(0, t.TempDir())
	m.inspector = &IPInspector{cache: map[string]IPInfo{}}
	nodes := []Node{}
	for i, name := range []string{"old-dead", "other-isp", "same-asn", "same-place-asn", "foreign"} {
		node := Node{HostName: name, IP: fmt.Sprintf("198.51.100.%d", 10+i), CountryCode: "JP"}
		nodes = append(nodes, node)
		m.inspector.cache[node.IP] = IPInfo{IP: node.IP, ASN: 2, CountryCode: "JP", Checked: time.Now()}
	}
	for _, i := range []int{0, 2, 3} {
		m.inspector.cache[nodes[i].IP] = IPInfo{IP: nodes[i].IP, ASN: 17511, CountryCode: "JP", Checked: time.Now()}
	}
	info := m.inspector.cache[nodes[3].IP]
	info.City = "Osaka"
	m.inspector.cache[nodes[3].IP] = info
	m.inspector.cache[nodes[4].IP] = IPInfo{IP: nodes[4].IP, ASN: 17511, CountryCode: "US", City: "Osaka", Checked: time.Now()}
	exit := &Tunnel{Slot: 1, Status: "starting", PreferredRegion: "JP", Node: nodes[0], PreferredNetwork: NetworkAffinity{ASN: 17511, CountryCode: "JP", City: "Osaka"}}
	m.tunnels[1] = exit
	m.candidateProbe = func(ctx context.Context, node Node) candidateCheck {
		if node.HostName == "old-dead" {
			return candidateCheck{Checked: true}
		}
		ms := 4.5
		return candidateCheck{Checked: true, Reachable: true, Latency: GateLatency{MS: &ms}}
	}
	got := m.preflightCandidates(exit, nodes)
	names := []string{}
	for _, node := range got {
		names = append(names, node.HostName)
	}
	if !reflect.DeepEqual(names, []string{"same-place-asn", "same-asn", "other-isp", "old-dead"}) {
		t.Fatal("wrong preferred reconnect order", names)
	}
}

func TestDev32PreflightBoundsConcurrencyAndStopsRemovedExit(t *testing.T) {
	m := NewManager(0, t.TempDir())
	m.probeSlots = make(chan struct{}, 4)
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	var mu sync.Mutex
	active, peak := 0, 0
	byExit := map[byte]int{}
	m.candidateProbe = func(ctx context.Context, node Node) candidateCheck {
		mu.Lock()
		active++
		byExit[node.HostName[0]]++
		if active > peak {
			peak = active
		}
		if active > 4 || byExit[node.HostName[0]] > 3 {
			t.Error("unbounded candidate probes", active, byExit)
		}
		mu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		active--
		byExit[node.HostName[0]]--
		mu.Unlock()
		return candidateCheck{}
	}
	done := make(chan []Node, 2)
	exits := []*Tunnel{}
	for i := 0; i < 2; i++ {
		exit := &Tunnel{Slot: i + 1, Status: "starting", PreferredRegion: "JP"}
		m.tunnels[exit.Slot] = exit
		exits = append(exits, exit)
	}
	for i, exit := range exits {
		nodes := make([]Node, 6)
		for j := range nodes {
			nodes[j] = Node{HostName: fmt.Sprintf("%c%d", 'a'+i, j), CountryCode: "JP"}
		}
		go func(exit *Tunnel, nodes []Node) { done <- m.preflightCandidates(exit, nodes) }(exit, nodes)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("candidate probes did not run concurrently")
		}
	}
	m.mu.Lock()
	delete(m.tunnels, exits[0].Slot)
	delete(m.tunnels, exits[1].Slot)
	m.mu.Unlock()
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case nodes := <-done:
			if len(nodes) != 0 {
				t.Error("removed exit continued reconnecting", nodes)
			}
		case <-time.After(time.Second):
			t.Fatal("candidate probes did not finish")
		}
	}
	if peak != 4 {
		t.Fatal("global probe limit was not exercised", peak)
	}
}

func TestDev32CandidateBatchDeduplicatesCapsAndHonorsQuota(t *testing.T) {
	useSettings(t)
	requests := 0
	s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		var ips []string
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&ips) != nil || len(ips) != 6 {
			t.Error("batch did not cap or deduplicate candidate IPs", ips)
		}
		w.Header().Set("X-Rl", "0")
		w.Header().Set("X-Ttl", "120")
		fmt.Fprint(w, `[{"status":"success","query":"198.51.100.1","as":"AS17511 OPTAGE","countryCode":"JP","regionName":"Osaka","city":"Osaka","hosting":false,"mobile":false},{"status":"success","query":"203.0.113.9","as":"AS1 Wrong","countryCode":"US"}]`)
	})
	s.batchAPI = s.ipapi + "batch"
	ips := []string{"127.0.0.1", "10.0.0.1", "198.51.100.1", "198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4", "198.51.100.5", "198.51.100.6", "198.51.100.7"}
	s.fillCandidateIdentities(context.Background(), ips)
	if info := s.get("198.51.100.1"); info == nil || info.ASN != 17511 || info.CountryCode != "JP" || info.City != "Osaka" {
		t.Fatal("candidate batch metadata lost", info)
	}
	if s.get("203.0.113.9") != nil {
		t.Fatal("unrequested IP injected into cache")
	}
	s.nextIdentityBatch = time.Time{}
	s.fillCandidateIdentities(context.Background(), ips)
	if requests != 1 || time.Until(s.blocked["ip-api.batch"]) < time.Minute {
		t.Fatal("candidate batch ignored provider quota", requests)
	}
}

func TestDev32ActualExitCountryMustBeKnownAndAffinityDoesNotDrift(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.inspector = &IPInspector{cache: map[string]IPInfo{}, pending: map[string]bool{}, queue: make(chan string, 8)}
	exit := &Tunnel{PreferredRegion: "JP", ExitIP: "198.51.100.8"}
	wrong := IPInfo{IP: exit.ExitIP, ASN: 17511, CountryCode: "US"}
	if err := m.validateExitCountry(exit, &wrong); err == nil {
		t.Fatal("foreign actual exit accepted")
	}
	if err := m.validateExitCountry(exit, nil); !errors.Is(err, errExitIdentityPending) || !m.inspector.pending[exit.ExitIP] {
		t.Fatal("unknown country accepted or not scheduled for retry", err)
	}
	m.inspector.cache[exit.ExitIP] = IPInfo{IP: exit.ExitIP, ASN: 17511, CountryCode: "JP", Checked: time.Now().Add(-25 * time.Hour)}
	if err := m.validateExitCountry(exit, nil); !errors.Is(err, errExitIdentityPending) {
		t.Fatal("expired country evidence accepted", err)
	}
	good := IPInfo{IP: exit.ExitIP, ASN: 17511, CountryCode: "JP", RegionName: "Osaka", City: "Osaka"}
	if err := m.validateExitCountry(exit, &good); err != nil {
		t.Fatal(err)
	}
	other := good
	other.ASN, other.City = 2516, "Tokyo"
	if err := m.validateExitCountry(exit, &other); err != nil {
		t.Fatal("same-country fallback disallowed", err)
	}
	if affinity := exit.networkAffinity(); affinity.ASN != good.ASN || affinity.City != good.City {
		t.Fatal("original network preference drifted after fallback", affinity)
	}
}

func TestDev32AffinityIsSavedAlongsideStableEntryIdentity(t *testing.T) {
	m := NewManager(0, t.TempDir())
	exit := &Tunnel{Slot: 1, Order: 9, Port: 30001, TemplateID: 7, Status: "starting", PreferredRegion: "JP", Node: Node{HostName: "jp-new", CountryCode: "JP", Config: "fixture"}, Cred: SocksCred{User: "fixture-user", Pass: "fixture-pass"}, PreferredNetwork: NetworkAffinity{ASN: 17511, CountryCode: "JP", Region: "Osaka", City: "Osaka"}}
	exit.setPrevHost("jp-old")
	m.tunnels[1] = exit
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath(m.workDir))
	var state persistedState
	if err != nil || json.Unmarshal(raw, &state) != nil || len(state.Tunnels) != 1 {
		t.Fatal("state not readable", err)
	}
	saved := state.Tunnels[0]
	if saved.PreferredNetwork != exit.networkAffinity() || saved.Port != 30001 || saved.Order != 9 || saved.SocksUser != exit.Cred.User || saved.SocksPass != exit.Cred.Pass || saved.PrevHost != "jp-old" || saved.TemplateID == nil || *saved.TemplateID != 7 {
		t.Fatal("reconnect preference or stable identity lost", saved)
	}
}
