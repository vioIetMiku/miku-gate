package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPing0GeoKeepsAttributeProvenanceAndParsesCallerIP(t *testing.T) {
	geo, err := parsePing0Geo("198.51.100.8\r\n日本 大阪\r\nAS17511\r\nOPTAGE Inc.\r\n")
	if err != nil || geo.ASN != 17511 || geo.Location != "日本 大阪" {
		t.Fatal(geo, err)
	}
	for _, raw := range []string{"<html>error</html>", "127.0.0.1\n日本\nAS1\nExample", "198.51.100.8\n日本\nASx\nExample", "198.51.100.8\n<script>\nAS1\nExample"} {
		if _, err := parsePing0Geo(raw); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	value := IPInfo{IP: geo.IP, Source: "ip-api.com", ASN: 1, Hosting: boolPtr(false), Mobile: boolPtr(false), Attribute: "非机房 / 非移动（住宅未确认）"}
	mergePing0Geo(&value, geo)
	if value.Source != "ip-api.com" || value.ASNSource != "ping0.cc" || value.ASN != geo.ASN || value.Hosting == nil || *value.Hosting {
		t.Fatal("attribute provenance lost", value)
	}
	mismatch := IPInfo{IP: "198.51.100.9", ASN: 2}
	mergePing0Geo(&mismatch, geo)
	if mismatch.ASN != 2 {
		t.Fatal("geo data attached to another IP")
	}
	geo.GeoChecked = time.Now().Add(-25 * time.Hour)
	mismatch.IP = geo.IP
	mergePing0Geo(&mismatch, geo)
	if mismatch.GeoSource != "" {
		t.Fatal("expired geo reused")
	}
}

func TestReconnectStaysInHomeCountryAndRepairsLegacyForeignSelection(t *testing.T) {
	withResidentialOnly(t, boolPtr(false))
	nodes := []Node{{HostName: "original", CountryCode: "JP"}, {HostName: "jp2", CountryCode: "JP", SpeedMbps: 10}, {HostName: "us-fast", CountryCode: "US", SpeedMbps: 1000}, {HostName: "kr1", CountryCode: "KR", SpeedMbps: 100}, {HostName: "kr-used", CountryCode: "KR", SpeedMbps: 90}, {HostName: "tw1", CountryCode: "TW", SpeedMbps: 80}, {HostName: "jp-avoid", CountryCode: "JP", SpeedMbps: 50}}
	m := mgrWith(nodes, "kr-used")
	m.workDir = t.TempDir()
	tunnel := &Tunnel{Slot: 2, Node: nodes[0], PreferredRegion: "JP", Status: "up"}
	m.tunnels[2] = tunnel
	tunnel.rememberSwap("jp-avoid")
	picks := m.candidatesFor(tunnel)
	if len(picks) != 2 || picks[0].HostName != "original" || picks[1].HostName != "jp2" {
		t.Fatal("wrong regional order", picks)
	}
	for _, node := range picks {
		if node.HostName == "us-fast" || node.HostName == "kr-used" || node.HostName == "jp-avoid" {
			t.Fatal("distant, used or avoided candidate", node)
		}
	}
	tunnel.Node = nodes[3]
	picks = m.candidatesFor(tunnel)
	if len(picks) == 0 || picks[0].CountryCode != "JP" {
		t.Fatal("home region drifted after failover", picks)
	}
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(statePath(m.workDir))
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	for _, saved := range state.Tunnels {
		if saved.Slot == 2 && saved.PreferredRegion != "JP" {
			t.Fatal("preferred region not saved")
		}
	}
}

func TestReconnectNeverReservesForeignTries(t *testing.T) {
	withResidentialOnly(t, boolPtr(false))
	nodes := []Node{{HostName: "current", CountryCode: "JP"}, {HostName: "kr", CountryCode: "KR"}, {HostName: "tw", CountryCode: "TW"}}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		nodes = append(nodes, Node{HostName: name, CountryCode: "JP", SpeedMbps: 100})
	}
	m := mgrWith(nodes)
	tunnel := &Tunnel{Slot: 1, Node: nodes[0], PreferredRegion: "JP"}
	m.tunnels[1] = tunnel
	picks := m.candidatesFor(tunnel)
	if len(picks) != 6 {
		t.Fatal("retry batch incomplete", picks)
	}
	for _, node := range picks {
		if node.CountryCode != "JP" {
			t.Fatal("foreign fallback", node)
		}
	}
}

func TestDefaultSourcesExcludeBrokenAPIOrigins(t *testing.T) {
	for _, source := range configuredSources() {
		if source == "https://vpngate.net/api/iphone/" || strings.HasPrefix(source, "http://150.40.") {
			t.Fatal("broken default restored", source)
		}
	}
}

func TestReconnectRotatesFailedAlternativesAndRetriesRecoveredNodes(t *testing.T) {
	withResidentialOnly(t, boolPtr(false))
	nodes := []Node{{HostName: "current", CountryCode: "JP"}, {HostName: "bad", CountryCode: "JP"}, {HostName: "fresh", CountryCode: "JP"}}
	m := mgrWith(nodes)
	tunnel := &Tunnel{Node: nodes[0], PreferredRegion: "JP"}
	tunnel.rememberFailedCandidate("bad")
	for _, node := range m.candidatesFor(tunnel) {
		if node.HostName == "bad" {
			t.Fatal("failed alternative immediately reused")
		}
	}
	tunnel.mu.Lock()
	tunnel.failedCandidates["bad"] = time.Now().Add(-failedCandidateCooldown - time.Second)
	tunnel.mu.Unlock()
	eligible := false
	for _, node := range m.candidatesFor(tunnel) {
		eligible = eligible || node.HostName == "bad"
	}
	if !eligible {
		t.Fatal("recovered alternative excluded forever")
	}
}
