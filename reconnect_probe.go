package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type candidateCheck struct {
	Checked   bool
	Reachable bool
	Latency   GateLatency
}

var errExitIdentityPending = errors.New("出口ASN属地暂未确认")

func checkCandidateEndpoint(ctx context.Context, node Node) candidateCheck {
	if ctx.Err() != nil {
		return candidateCheck{}
	}
	endpoints := nodeProbeEndpoints(node)
	checked := false
	for _, ep := range endpoints {
		if !strings.HasPrefix(ep.Proto, "tcp") {
			continue
		}
		checked = true
		ms, err := tcpEndpointLatency(ctx, ep)
		if err == nil {
			return candidateCheck{Checked: true, Reachable: true, Latency: GateLatency{MS: &ms, Target: ep.IP, Port: ep.Port, Method: "tcp", Checked: time.Now()}}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if checked {
		return candidateCheck{Checked: true}
	}
	// UDP or a lazy profile: an ICMP timeout is inconclusive, not rejection.
	if target := gatePingTarget(node); target != "" {
		if ms, err := pingGateContext(ctx, target); err == nil {
			return candidateCheck{Reachable: true, Latency: GateLatency{MS: &ms, Target: target, Method: "icmp", Checked: time.Now()}}
		}
	}
	return candidateCheck{}
}

func (m *Manager) preflightCandidates(t *Tunnel, nodes []Node) []Node {
	if len(nodes) == 0 || m.latencies == nil && m.candidateProbe == nil {
		return nodes
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	checks := make([]candidateCheck, len(nodes))
	metadataDone := make(chan struct{})
	go func() {
		defer close(metadataDone)
		if m.inspector != nil {
			addresses := []string{}
			for _, n := range nodes {
				addresses = append(addresses, gatePingTarget(n))
			}
			m.inspector.fillCandidateIdentities(ctx, addresses)
		}
	}()
	probe := m.candidateProbe
	if probe == nil {
		probe = checkCandidateEndpoint
	}
	localLimit := make(chan struct{}, 3)
	for index, node := range nodes {
		wg.Add(1)
		go func(index int, node Node) {
			defer wg.Done()
			select {
			case localLimit <- struct{}{}:
				defer func() { <-localLimit }()
			case <-ctx.Done():
				return
			}
			if m.probeSlots != nil {
				select {
				case m.probeSlots <- struct{}{}:
					defer func() { <-m.probeSlots }()
				case <-ctx.Done():
					return
				}
			}
			if ctx.Err() == nil {
				checks[index] = probe(ctx, node)
			}
		}(index, node)
	}
	wg.Wait()
	<-metadataDone
	if !m.tunnelActive(t) {
		return nil
	}
	preferred := t.networkAffinity()
	if preferred.CountryCode == "" {
		preferred.CountryCode = t.preferredRegion()
	}
	type ranked struct {
		node  Node
		check candidateCheck
		rank  int
		index int
	}
	list := []ranked{}
	for i, node := range nodes {
		var info *IPInfo
		if m.inspector != nil {
			info = m.inspector.networkInfo(gatePingTarget(node))
		}
		if info != nil {
			if code := infoCountryCode(*info); code != "" && t.preferredRegion() != "" && code != t.preferredRegion() {
				continue
			}
		}
		list = append(list, ranked{node: node, check: checks[i], rank: affinityRank(preferred, info), index: i})
		if m.latencies != nil && checks[i].Reachable && checks[i].Latency.MS != nil {
			m.latencies.mu.Lock()
			m.latencies.cache[latencyKey(node)] = checks[i].Latency
			m.latencies.mu.Unlock()
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		af, bf := a.check.Checked && !a.check.Reachable, b.check.Checked && !b.check.Reachable
		if af != bf {
			return !af
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.check.Reachable != b.check.Reachable {
			return a.check.Reachable
		}
		if a.index == 0 || b.index == 0 {
			return a.index == 0
		}
		if (a.check.Latency.MS != nil) != (b.check.Latency.MS != nil) {
			return a.check.Latency.MS != nil
		}
		if a.check.Latency.MS != nil && b.check.Latency.MS != nil && *a.check.Latency.MS != *b.check.Latency.MS {
			return *a.check.Latency.MS < *b.check.Latency.MS
		}
		return a.index < b.index
	})
	out := make([]Node, 0, len(list))
	for _, item := range list {
		out = append(out, item.node)
	}
	return out
}

func (m *Manager) validateExitCountry(t *Tunnel, geo *IPInfo) error {
	region := t.preferredRegion()
	if region == "" {
		return nil
	}
	if geo != nil {
		if code := infoCountryCode(*geo); code != "" {
			if code != region {
				return fmt.Errorf("实际出口ASN属地为%s，继续寻找%s节点", code, region)
			}
			if geo.ASN != 0 {
				t.rememberAffinity(*geo)
				return nil
			}
		}
	}
	if m.inspector == nil {
		return nil
	}
	info := m.inspector.networkInfo(t.ExitIP)
	if info == nil || infoCountryCode(*info) == "" || info.ASN == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		m.inspector.fillCandidateIdentities(ctx, []string{t.ExitIP})
		cancel()
		info = m.inspector.networkInfo(t.ExitIP)
	}
	if info != nil {
		code := infoCountryCode(*info)
		if code != "" && code != region {
			return fmt.Errorf("实际出口ASN属地为%s，继续寻找%s节点", code, region)
		}
		if code == region && info.ASN != 0 {
			t.rememberAffinity(*info)
			return nil
		}
	}
	m.inspector.enqueue(t.ExitIP)
	return fmt.Errorf("%w，等待来源恢复后重试%s节点", errExitIdentityPending, region)
}
