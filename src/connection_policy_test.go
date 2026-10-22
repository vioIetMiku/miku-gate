package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHealthTransientFailureRecoversWithoutReconnect(t *testing.T) {
	now := time.Now()
	task := healthTask{Tunnel: &Tunnel{}, ExitIP: "198.51.100.8", Node: Node{HostName: "original"}, ReadyAt: now}
	state := healthFailureState{}
	bad := healthResult{Task: task}
	for _, offset := range []time.Duration{0, 10, 20, 50, 70} {
		if state.observe(bad, now.Add(offset*time.Second)) {
			t.Fatal("brief outage tore down a recovering connection", offset)
		}
	}
	if state.observe(healthResult{Task: task, Healthy: true}, now.Add(74*time.Second)) || state.count != 0 {
		t.Fatal("verified recovery failed to reset outage")
	}
	for _, offset := range []time.Duration{80, 90, 100, 150} {
		if state.observe(bad, now.Add(offset*time.Second)) {
			t.Fatal("old outage shortened a new recovery window", offset)
		}
	}
	if !state.observe(bad, now.Add(155*time.Second)) {
		t.Fatal("sustained outage never recovered")
	}
}

func TestHealthRequiresRepeatedFailureAndResetsForNewConnection(t *testing.T) {
	now := time.Now()
	task := healthTask{Tunnel: &Tunnel{}, ExitIP: "198.51.100.8", Node: Node{HostName: "same"}, ReadyAt: now}
	state := healthFailureState{}
	bad := healthResult{Task: task}
	if state.observe(bad, now) || state.observe(bad, now.Add(time.Minute*3)) {
		t.Fatal("one or two delayed samples caused a reconnect")
	}
	bad.Task.ReadyAt = now.Add(time.Minute)
	if state.observe(bad, now.Add(time.Minute*4)) || state.count != 1 {
		t.Fatal("same-node recovery inherited stale failures")
	}
	if !state.observe(healthResult{Task: bad.Task, IdentityMismatch: true}, now.Add(time.Minute*4)) {
		t.Fatal("known incorrect exit was hidden by grace")
	}
}

func TestHealthFallsBackWhenBothIPServicesAreUnavailable(t *testing.T) {
	task := healthTask{ExitIP: "198.51.100.8"}
	calls := 0
	result := probeTunnelHealthWith(task, func(_, address string) (string, float64, error) {
		calls++
		if address != "https://checkip.amazonaws.com" {
			return "", 0, errors.New("service unavailable")
		}
		return task.ExitIP + "\n", 22, nil
	})
	if !result.Healthy || calls != 3 || result.IdentityMismatch {
		t.Fatal("external lookup failures caused a false outage", result, calls)
	}
}

func TestHealthOldResultCannotRestartSameEndpointAgain(t *testing.T) {
	m := NewManager(0, t.TempDir())
	exit := &Tunnel{Slot: 1, Status: "up", ExitIP: "198.51.100.8", Node: Node{HostName: "same"}, readyAt: time.Now().Add(-time.Minute)}
	m.tunnels[1] = exit
	old, _ := m.healthSnapshot(exit)
	if !m.acceptExitIP(exit) {
		t.Fatal("fixture could not reconnect")
	}
	if m.healthResultCurrent(healthResult{Task: old}) || m.beginReconnect(exit, &old) {
		t.Fatal("old measurement restarted a recovered connection")
	}
}

func TestManagedVPNPolicyPreservesPKIAndKeepsSoftRecovery(t *testing.T) {
	raw := "client\nremote 8.8.8.8 443\nproto tcp-client\nping-exit 10\nkeepalive 1 5\n--ping-restart 5\n\"inactive\" 10\nhand-window 5\nreneg-sec 3600\n<key>\nping-exit\nKEEP KEY BYTES\n</key>\n"
	profile := managedVPNProfile(raw)
	if !strings.Contains(profile, "<key>\nping-exit\nKEEP KEY BYTES\n</key>") || !strings.Contains(profile, "reneg-sec 3600") || strings.Contains(profile, "ping-exit 10") || strings.Contains(profile, "keepalive 1 5") || strings.Contains(profile, "inactive") || strings.Contains(profile, "hand-window") {
		t.Fatal("managed policy corrupted PKI or left conflicting liveness options", profile)
	}
	if err := validateVPNProfile(profile); err != nil {
		t.Fatal("managed client profile rejected", err)
	}
	for _, candidate := range []bool{false, true} {
		exit := &Tunnel{Slot: 1}
		exit.quickCandidate.Store(candidate)
		args := strings.Join(exit.openVPNArgs("profile", "auth", "log"), " ")
		if !strings.Contains(args, "--persist-tun --persist-key") || !strings.Contains(args, "--ping 10 --ping-restart 45") || strings.Contains(args, "--hand-window") || !strings.Contains(args, "--connect-retry 1 8") {
			t.Fatal("connection lost bounded soft recovery", args)
		}
	}
}

func TestReconnectPrefersOriginalReachableNodeOverSlightlyFasterAlternative(t *testing.T) {
	m := NewManager(0, t.TempDir())
	old := Node{HostName: "original", CountryCode: "JP", IP: "198.51.100.1"}
	other := Node{HostName: "faster", CountryCode: "JP", IP: "198.51.100.2"}
	exit := &Tunnel{Slot: 1, Status: "starting", Node: old, PreferredRegion: "JP"}
	exit.PreferredNetwork = NetworkAffinity{CountryCode: "JP", ASN: 2}
	m.inspector = &IPInspector{cache: map[string]IPInfo{
		old.IP:   {IP: old.IP, CountryCode: "JP", ASN: 1, Checked: time.Now()},
		other.IP: {IP: other.IP, CountryCode: "JP", ASN: 2, Checked: time.Now()},
	}}
	m.tunnels[1] = exit
	m.candidateProbe = func(_ context.Context, node Node) candidateCheck {
		ms := 10.0
		if node.HostName == old.HostName {
			ms = 100
		}
		return candidateCheck{Checked: true, Reachable: true, Latency: GateLatency{MS: &ms}}
	}
	got := m.preflightCandidates(exit, []Node{old, other})
	if len(got) != 2 || got[0].HostName != old.HostName {
		t.Fatal("latency optimization unnecessarily switched the original node", got)
	}
}
