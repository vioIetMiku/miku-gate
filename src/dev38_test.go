package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func raceFixtureNodes() []Node {
	var nodes []Node
	for r, region := range quickRegions {
		for i := 1; i <= 18; i++ {
			nodes = append(nodes, Node{HostName: fmt.Sprintf("%s-%d", region.Code, i), CountryCode: region.Code, IP: fmt.Sprintf("198.51.%d.%d", 100+r, i)})
		}
	}
	return nodes
}

func TestDev40QuickRaceTriplesCandidatesWithoutStarvingCountries(t *testing.T) {
	useSettings(t)
	for _, tc := range []struct {
		limit int
		want  map[string]int
	}{{0, map[string]int{"JP": 15, "KR": 9, "US": 9}}, {11, map[string]int{"JP": 5, "KR": 3, "US": 3}}, {14, map[string]int{"JP": 6, "KR": 4, "US": 4}}} {
		m := NewManager(tc.limit, t.TempDir())
		m.nodes = raceFixtureNodes()
		m.nodes = append(m.nodes, Node{HostName: "duplicate", CountryCode: "US", IP: m.nodes[0].IP})
		targets := m.quickTargets(quickFixtureJob(m), 77)
		counts, endpoints := m.quickRaceLimits(targets), map[string]bool{}
		for _, node := range m.quickRacePicks(targets) {
			if endpoints[node.IP] {
				t.Fatal("duplicate endpoint", node)
			}
			endpoints[node.IP] = true
		}
		for code, want := range tc.want {
			if counts[code] != want {
				t.Fatal(tc.limit, code, counts, tc.want)
			}
		}
	}
}

func awaitRaceValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("parallel race did not make progress")
		var zero T
		return zero
	}
}

func TestDev38QuickRaceKeepsFirstArrivalsAndCancelsSlowLosers(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = raceFixtureNodes()
	job := quickFixtureJob(m)
	targets := m.quickTargets(job, 77)
	started, entered, stopped := make(chan string, 33), make(chan string, 33), make(chan string, 33)
	release := map[string]chan struct{}{}
	for _, node := range m.quickRacePicks(targets) {
		release[node.HostName] = make(chan struct{})
	}
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(job, targets, 77, quickRaceOps{
			connect: func(tunnel *Tunnel) error {
				started <- tunnel.Node.HostName
				select {
				case <-release[tunnel.Node.HostName]:
					tunnel.ExitIP = tunnel.Node.IP
					return nil
				case <-tunnel.connectionContext().Done():
					return context.Canceled
				}
			},
			entry: func(tunnel *Tunnel) error {
				tunnel.EntryReady = true
				entered <- tunnel.Node.HostName
				return nil
			},
			stop: func(tunnel *Tunnel) { tunnel.Status = "stopped"; stopped <- tunnel.Node.HostName },
		})
	}()
	for i := 0; i < 33; i++ {
		awaitRaceValue(t, started)
	}
	if len(m.visibleTunnels()) != 0 {
		t.Fatal("background candidates appeared in the UI")
	}
	// Deliberately finish the initially lower-ranked candidates first.
	winners := []string{"JP-10", "KR-6", "US-6", "JP-9", "KR-5", "US-5", "JP-8", "KR-4", "US-4", "JP-7", "JP-6"}
	for _, host := range winners {
		close(release[host])
		if got := awaitRaceValue(t, entered); got != host {
			t.Fatal("did not process the first arrival", got, host)
		}
	}
	cleaned := awaitRaceValue(t, returned)
	awaitRaceValue(t, cleaned)
	if len(m.Tunnels()) != 11 || len(m.visibleTunnels()) != 11 {
		t.Fatal("extra connections remained", len(m.Tunnels()))
	}
	seen := map[string]bool{}
	for _, tunnel := range m.visibleTunnels() {
		seen[tunnel.Node.HostName] = true
		if tunnel.connectionContext().Err() != nil {
			t.Fatal("winner inherited canceled candidate context")
		}
	}
	for _, host := range winners {
		if !seen[host] {
			t.Fatal("a slower candidate replaced a winner", host)
		}
	}
	if len(stopped) != 22 {
		t.Fatal("slow candidates were not cleaned", len(stopped))
	}
	for _, step := range job.View().Steps[quickExitStep:quickSummaryStep] {
		if step.Status != "ok" {
			t.Fatal("target was not completed", step)
		}
	}
	raw, err := os.ReadFile(statePath(m.workDir))
	var state persistedState
	if err != nil || json.Unmarshal(raw, &state) != nil || len(state.Tunnels) != 11 {
		t.Fatal("speculative connections leaked into restart state", err, string(raw))
	}
}

func TestDev38QuickRaceShortagesAndEntryFailuresStayInTheirCountry(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = raceFixtureNodes()
	job := quickFixtureJob(m)
	targets := []quickTarget{{"JP", 4}, {"KR", 9}, {"US", 12}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var entryMu sync.Mutex
	krEntries := 0
	cleaned := m.runQuickRace(job, targets, 77, quickRaceOps{
		ctx: ctx, retryDelay: time.Hour,
		connect: func(tunnel *Tunnel) error {
			if tunnel.Node.CountryCode != "KR" {
				return errors.New("fixture unreachable")
			}
			tunnel.ExitIP = tunnel.Node.IP
			return nil
		},
		entry: func(tunnel *Tunnel) error {
			entryMu.Lock()
			defer entryMu.Unlock()
			krEntries++
			if krEntries == 1 {
				return errors.New("fixture entry failure")
			}
			tunnel.EntryReady = true
			return nil
		},
		stop: func(tunnel *Tunnel) { tunnel.Status = "stopped" },
	})
	awaitRaceValue(t, cleaned)
	visible := m.visibleTunnels()
	if len(visible) != 1 || visible[0].preferredRegion() != "KR" || krEntries != 2 {
		t.Fatal("failed entrance consumed a quota or countries were mixed", len(visible), krEntries)
	}
	view := job.View()
	if view.Steps[4].Status != "failed" || view.Steps[9].Status != "ok" || view.Steps[12].Status != "failed" {
		t.Fatal("shortages were hidden", view)
	}
}

func TestDev38QuickCandidateCannotSurviveStopOrLeakToSubscription(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	candidate, err := m.prepareTunnelMode(Node{HostName: "hidden", CountryCode: "JP"}, 2, 77, false, ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Status, candidate.EntryReady = "up", true
	visible := &Tunnel{Slot: 99, Status: "up", Node: Node{HostName: "visible"}}
	m.tunnels[99] = visible
	usePanel(t, &Native{store: &nativeStore{Inbounds: []*nativeInbound{
		{ID: 1, Port: 30001, Protocol: "vless", Enable: true, BoundTo: "hidden", Clients: []nativeClient{{ID: "hidden-client", Enable: true}}},
		{ID: 2, Port: 30002, Protocol: "vless", Enable: true, BoundTo: "visible", Clients: []nativeClient{{ID: "visible-client", Enable: true}}},
	}}})
	links, err := subLinks(m, "192.0.2.10", true)
	if err != nil || len(links) != 1 {
		t.Fatal("candidate leaked into subscription", links, err)
	}
	w := httptest.NewRecorder()
	apiSubscriptionLinks(m).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sub/links", nil))
	var body struct {
		Links []string `json:"links"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != 200 || len(body.Links) != 1 {
		t.Fatal("completion links do not match subscription", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	apiTunnels(m).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tunnels", nil))
	var tunnels []Tunnel
	if json.Unmarshal(w.Body.Bytes(), &tunnels) != nil || len(tunnels) != 1 || tunnels[0].Node.HostName != "visible" {
		t.Fatal("candidate leaked into raw tunnel API", w.Body.String())
	}
	if err := m.Stop(candidate.Slot); err != nil || m.promoteQuickCandidate(candidate) || candidate.connectionContext().Err() == nil {
		t.Fatal("stopped candidate could still be published", err)
	}
	if m.tunnels[candidate.Slot] != candidate {
		t.Fatal("candidate slot was released before its worker cleanup")
	}
}

func TestDev38QuickResultSnapshotDoesNotShareMutableRegions(t *testing.T) {
	job := (&JobStore{}).New("quick", nil)
	result := QuickSetupResult{Ready: 11, Target: 11, Regions: []QuickRegionResult{{Code: "JP", Name: "日本", Ready: 5, Target: 5}}}
	job.SetQuickResult(result)
	result.Regions[0].Ready = 0
	view := job.View()
	if view.QuickResult.Regions[0].Ready != 5 {
		t.Fatal("input modified saved result")
	}
	view.QuickResult.Regions[0].Ready = 1
	if job.View().QuickResult.Regions[0].Ready != 5 {
		t.Fatal("view modified saved result")
	}
}

func TestDev38QuickRaceFinishesBeforeSlowWorkerAndKeepsSlotReserved(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = raceFixtureNodes()
	m.nodes = m.nodes[:2] // Isolate one blocked worker and one winning worker.
	job := quickFixtureJob(m)
	slowRelease := make(chan struct{})
	startRelease := make(chan struct{})
	started := make(chan string, 2)
	stopped := make(chan string, 2)
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(job, []quickTarget{{"JP", 4}}, 77, quickRaceOps{
			connect: func(tunnel *Tunnel) error {
				started <- tunnel.Node.HostName
				<-startRelease
				if tunnel.Node.HostName == "JP-1" {
					<-slowRelease // Simulate a system operation finishing after cancellation.
					return context.Canceled
				}
				tunnel.ExitIP = tunnel.Node.IP
				return nil
			},
			entry: func(tunnel *Tunnel) error { tunnel.EntryReady = true; return nil },
			stop:  func(tunnel *Tunnel) { stopped <- tunnel.Node.HostName },
		})
	}()
	awaitRaceValue(t, started)
	awaitRaceValue(t, started)
	close(startRelease)
	cleaned := awaitRaceValue(t, returned)
	if len(m.visibleTunnels()) != 1 || m.visibleTunnels()[0].Node.HostName != "JP-2" {
		t.Fatal("slow connection delayed the winning node")
	}
	select {
	case <-stopped:
		t.Fatal("resources were removed before their worker exited")
	default:
	}
	m.mu.RLock()
	var oldSlot int
	for slot, tunnel := range m.tunnels {
		if tunnel.Node.HostName == "JP-1" {
			oldSlot = slot
		}
	}
	m.mu.RUnlock()
	if oldSlot == 0 {
		t.Fatal("canceled slot was released too early")
	}
	newTunnel, err := m.prepareTunnel(Node{HostName: "new-exit", CountryCode: "KR"}, 0, 0, false)
	if err != nil || newTunnel.Slot == oldSlot {
		t.Fatal("new exit reused a namespace still being cleaned", err)
	}
	close(slowRelease)
	awaitRaceValue(t, cleaned)
	if m.tunnels[newTunnel.Slot] != newTunnel {
		t.Fatal("old cleanup removed a new exit")
	}
}
