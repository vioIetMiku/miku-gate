package main

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDev33StabilityNoticeStartsOnFourthReconnectAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	exit := &Tunnel{}
	for _, ago := range []time.Duration{9 * time.Minute, 8 * time.Minute, 7 * time.Minute} {
		exit.recordAutoReconnect(now.Add(-ago))
	}
	if exit.stabilityNotice(now) != nil {
		t.Fatal("three reconnects should not display a notice")
	}
	exit.recordAutoReconnect(now.Add(-time.Minute))
	notice := exit.stabilityNotice(now)
	if notice == nil || notice.Reconnects != 4 || notice.WindowSeconds != 600 || notice.Message != reconnectNoticeMessage || !notice.Until.Equal(now.Add(time.Minute)) {
		t.Fatal("four reconnects did not display the expected notice", notice)
	}
	if exit.stabilityNotice(now.Add(time.Minute)) != nil {
		t.Fatal("notice survived the sliding-window boundary")
	}
	if history := exit.reconnectHistory(now.Add(10 * time.Minute)); len(history) != 0 {
		t.Fatal("expired events remained in the history", history)
	}
}

func TestDev33ReconnectHistoryIgnoresExpiredFutureAndOtherExitEvents(t *testing.T) {
	now := time.Now()
	events := []time.Time{now.Add(-time.Minute), now.Add(-10 * time.Minute), now.Add(time.Minute), now.Add(-3 * time.Minute), now.Add(-2 * time.Minute), now}
	exit := &Tunnel{Slot: 1}
	exit.restoreReconnectHistory(events, now)
	if notice := exit.stabilityNotice(now); notice == nil || notice.Reconnects != 4 {
		t.Fatal("restore did not retain exactly the valid events", notice)
	}
	copy := exit.reconnectHistory(now)
	copy[0] = now.Add(-time.Hour)
	if exit.stabilityNotice(now) == nil {
		t.Fatal("history getter exposed mutable internal storage")
	}
	if (&Tunnel{Slot: 1}).stabilityNotice(now) != nil {
		t.Fatal("reused slot inherited the previous exit's history")
	}
}

func TestDev33OnlyAcceptedHealthReconnectsAreCounted(t *testing.T) {
	m := NewManager(0, t.TempDir())
	exit := &Tunnel{Slot: 1, Status: "up", ExitIP: "198.51.100.8", Node: Node{HostName: "jp", IP: "203.0.113.8", CountryCode: "JP"}}
	m.tunnels[1] = exit
	if !m.beginReconnect(exit, nil) || len(exit.reconnectHistory(time.Now())) != 0 {
		t.Fatal("manual reconnect was counted as an outage")
	}
	exit.Status, exit.ExitIP = "up", "198.51.100.8"
	task, _ := m.healthSnapshot(exit)
	stale := task
	stale.ExitIP = "198.51.100.9"
	if m.beginReconnect(exit, &stale) || len(exit.reconnectHistory(time.Now())) != 0 {
		t.Fatal("stale health result counted as a reconnect")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.beginReconnect(exit, &task) {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || len(exit.reconnectHistory(time.Now())) != 1 {
		t.Fatal("duplicate health results counted the same outage repeatedly", successes.Load())
	}
	exit.Status, exit.ExitIP = "stopped", task.ExitIP
	if m.beginReconnect(exit, &task) || len(exit.reconnectHistory(time.Now())) != 1 {
		t.Fatal("stopped exit received another reconnect event")
	}
}

func TestDev33ReconnectNoticeSurvivesStateRoundTrip(t *testing.T) {
	now := time.Now()
	m := NewManager(0, t.TempDir())
	exit := &Tunnel{Slot: 1, Status: "starting", Port: 30001, Node: Node{HostName: "jp-new", CountryCode: "JP"}}
	for i := 4; i > 0; i-- {
		exit.recordAutoReconnect(now.Add(-time.Duration(i) * time.Minute))
	}
	exit.setPrevHost("jp-old")
	m.tunnels[1] = exit
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath(m.workDir))
	var state persistedState
	if err != nil || json.Unmarshal(raw, &state) != nil || len(state.Tunnels) != 1 {
		t.Fatal("persisted reconnect history was unreadable", err)
	}
	copy := &Tunnel{}
	copy.restoreReconnectHistory(state.Tunnels[0].AutoReconnects, now)
	if notice := copy.stabilityNotice(now); notice == nil || notice.Reconnects != 4 || state.Tunnels[0].PrevHost != "jp-old" || state.Tunnels[0].Port != 30001 {
		t.Fatal("history or existing reconnect identity lost on save", notice)
	}
	if copy.stabilityNotice(now.Add(10*time.Minute)) != nil {
		t.Fatal("restored history never expired")
	}
	var legacy persistedTunnel
	if json.Unmarshal([]byte(`{"slot":1,"hostname":"old"}`), &legacy) != nil || len(legacy.AutoReconnects) != 0 {
		t.Fatal("legacy state acquired fabricated reconnects")
	}
}

func TestDev33GateLatencyQueuesOnlyOncePerThirtySeconds(t *testing.T) {
	for _, failed := range []bool{false, true} {
		s := latencyFixture()
		node := Node{IP: "198.51.100.8"}
		ms := 2.25
		value := GateLatency{MS: &ms, Target: node.IP, Method: "icmp", Checked: time.Now().Add(-29 * time.Second)}
		if failed {
			value.MS, value.Error = nil, "temporarily unavailable"
		}
		s.cache[latencyKey(node)] = value
		for i := 0; i < 20; i++ {
			s.get(node)
		}
		if len(s.queue) != 0 {
			t.Fatal("page polls triggered another measurement before 30 seconds", failed)
		}
		value.Checked = time.Now().Add(-31 * time.Second)
		s.cache[latencyKey(node)] = value
		for i := 0; i < 20; i++ {
			s.get(node)
		}
		if len(s.queue) != 1 {
			t.Fatal("due measurement was missing or queued repeatedly", failed, len(s.queue))
		}
	}
}

func TestDev33HealthTimingDoesNotRefreshLatencyBeforeThirtySeconds(t *testing.T) {
	s := latencyFixture()
	node := Node{IP: "198.51.100.8"}
	now := time.Now()
	s.recordTunnelSampleAt(node, 100, "https://api.ipify.org", now)
	s.recordTunnelSampleAt(node, 200, "https://ping0.cc/geo", now.Add(10*time.Second))
	s.recordTunnelSampleAt(node, 300, "https://api.ipify.org", now.Add(29*time.Second))
	first := s.tunnelSamples[latencyKey(node)]
	if first.MS == nil || *first.MS != 100 || !first.Checked.Equal(now) {
		t.Fatal("health checks bypassed the latency update interval", first)
	}
	s.recordTunnelSampleAt(node, 400, "https://ping0.cc/geo", now.Add(30*time.Second))
	second := s.tunnelSamples[latencyKey(node)]
	if second.MS == nil || *second.MS != 400 || !second.Checked.Equal(now.Add(30*time.Second)) {
		t.Fatal("due tunnel measurement was not published", second)
	}
	changed := Node{IP: "198.51.100.9"}
	s.recordTunnelSampleAt(changed, 500, "https://api.ipify.org", now.Add(31*time.Second))
	if sample := s.tunnelSamples[latencyKey(changed)]; sample.MS == nil || *sample.MS != 500 {
		t.Fatal("previous endpoint's throttle blocked a new endpoint")
	}
}
