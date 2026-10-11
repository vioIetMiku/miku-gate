package store

import (
	"mikugate/internal/domain"
	"mikugate/internal/vpngate"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryExplicitSameCountryFreshnessAndPausePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	exit, _, _ := s.CreateExit(domain.ExitInput{Name: "recovery", Country: "JP"})
	nodes := []domain.Node{{ID: "jp", Country: "JP", Source: vpngate.Source, FetchedAt: time.Now()}, {ID: "us", Country: "US", Source: vpngate.Source, FetchedAt: time.Now()}, {ID: "stale", Country: "JP", Source: vpngate.Source, FetchedAt: time.Now().Add(-48 * time.Hour)}}
	s.ReplaceNodes(t.Context(), nodes)
	if e = s.SaveRecovery(exit.ID, domain.RecoveryPolicy{Enabled: true, NodeIDs: []string{"jp"}}); e == nil {
		t.Fatal("no consent saved")
	}
	for _, id := range []string{"us", "stale", "missing"} {
		if e = s.SaveRecovery(exit.ID, domain.RecoveryPolicy{Enabled: true, Confirm: true, NodeIDs: []string{id}}); e == nil {
			t.Fatal("invalid candidate saved", id)
		}
	}
	if e = s.SaveRecovery(exit.ID, domain.RecoveryPolicy{Enabled: true, Confirm: true, NodeIDs: []string{"jp"}}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Recovery(exit.ID)
	if e != nil || !p.Enabled || len(p.NodeIDs) != 1 {
		t.Fatal("policy not durable", e)
	}
	if e = s.PauseRecovery(exit.ID); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Recovery(exit.ID)
	if p.Enabled {
		t.Fatal("user pause not saved")
	}
	if e = s.SaveUpstream(exit.ID, domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443}); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveRecovery(exit.ID, domain.RecoveryPolicy{Enabled: true, Confirm: true, NodeIDs: []string{"jp"}}); e == nil {
		t.Fatal("proxy silently replaced by VPN automatic switching")
	}
}
