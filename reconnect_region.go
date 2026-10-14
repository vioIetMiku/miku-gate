package main

import (
	"sort"
	"strings"
)

func normalizeRegion(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return code
}
func (t *Tunnel) preferredRegion() string {
	if region := normalizeRegion(t.PreferredRegion); region != "" {
		return region
	}
	return normalizeRegion(t.Node.CountryCode)
}

// The original country remains a constraint, even after a restart or an older
// cross-country failover. An exhausted country waits; it never changes country.
func (m *Manager) regionalCandidates(t *Tunnel) []Node {
	const maxTries = 6
	avoid := t.swapAvoid()
	preferred := t.networkAffinity()
	if preferred.CountryCode == "" {
		preferred.CountryCode = t.preferredRegion()
	}
	m.mu.RLock()
	region, first := t.preferredRegion(), t.Node
	for _, node := range m.nodes {
		if node.HostName == first.HostName {
			first = node
			break
		}
	}
	used := map[string]bool{}
	usedIPs := map[string]bool{}
	for _, other := range m.tunnels {
		if other != t {
			used[other.Node.HostName] = true
			if other.Node.IP != "" {
				usedIPs[other.Node.IP] = true
			}
		}
	}
	pool := append([]Node(nil), m.nodePoolLocked()...)
	m.mu.RUnlock()
	out := []Node{}
	if !used[first.HostName] && !usedIPs[first.IP] && !t.candidateFailedRecently(first.HostName) && (region == "" || normalizeRegion(first.CountryCode) == region) {
		out = append(out, first)
		if first.IP != "" {
			usedIPs[first.IP] = true
		}
	}
	used[first.HostName] = true
	alternatives := []Node{}
	for _, node := range pool {
		if used[node.HostName] || usedIPs[node.IP] || avoid[node.HostName] || t.candidateFailedRecently(node.HostName) {
			continue
		}
		if region != "" && normalizeRegion(node.CountryCode) != region {
			continue
		}
		alternatives = append(alternatives, node)
		used[node.HostName] = true
		if node.IP != "" {
			usedIPs[node.IP] = true
		}
	}
	if m.inspector != nil {
		ranks := map[string]int{}
		for _, node := range alternatives {
			ranks[node.HostName] = affinityRank(preferred, m.inspector.networkInfo(gatePingTarget(node)))
		}
		sort.SliceStable(alternatives, func(i, j int) bool { return ranks[alternatives[i].HostName] < ranks[alternatives[j].HostName] })
	}
	for _, node := range alternatives {
		if len(out) == maxTries {
			break
		}
		out = append(out, node)
	}
	return out
}
