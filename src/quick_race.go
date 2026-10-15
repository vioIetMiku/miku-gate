package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type quickTarget struct {
	code string
	step int
}

// Consider the whole pool. Limits constrain concurrent workers, never attempts.
func (m *Manager) quickRacePicks(targets []quickTarget) []Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wanted, used, ips := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, target := range targets {
		wanted[target.code] = true
	}
	for _, t := range m.tunnels {
		used[t.Node.HostName] = true
		if t.Node.IP != "" {
			ips[t.Node.IP] = true
		}
	}
	var picks []Node
	for _, node := range m.nodePoolLocked() {
		if node.HostName == "" || !wanted[normalizeRegion(node.CountryCode)] || used[node.HostName] || node.IP != "" && ips[node.IP] {
			continue
		}
		picks = append(picks, node)
		used[node.HostName] = true
		if node.IP != "" {
			ips[node.IP] = true
		}
	}
	// Prefer recent measurements from this VPS; source-advertised speed alone
	// does not predict how quickly a connection can be established here.
	if m.latencies != nil {
		m.latencies.mu.Lock()
		samples := make(map[string]GateLatency, len(picks))
		for _, node := range picks {
			samples[node.HostName] = m.latencies.cache[latencyKey(node)]
		}
		m.latencies.mu.Unlock()
		sort.SliceStable(picks, func(i, j int) bool {
			a, b := samples[picks[i].HostName], samples[picks[j].HostName]
			av := a.MS != nil && time.Since(a.Checked) < 90*time.Second
			bv := b.MS != nil && time.Since(b.Checked) < 90*time.Second
			if av != bv {
				return av
			}
			if av && *a.MS != *b.MS {
				return *a.MS < *b.MS
			}
			return false
		})
	}
	return picks
}

const quickRaceMultiplier = 3

// Reserve every country's target capacity before sharing spare workers.
func (m *Manager) quickRaceLimits(targets []quickTarget) map[string]int {
	m.mu.RLock()
	capacity := linkSubnetCapacity
	if m.maxSlots > 0 && m.maxSlots < capacity {
		capacity = m.maxSlots
	}
	room := capacity - len(m.tunnels) - len(targets)
	m.mu.RUnlock()
	counts, limits := map[string]int{}, map[string]int{}
	for _, target := range targets {
		counts[target.code]++
		limits[target.code]++
	}
	for room > 0 {
		added := false
		for _, region := range quickRegions {
			if room == 0 || limits[region.Code] >= counts[region.Code]*quickRaceMultiplier {
				continue
			}
			limits[region.Code]++
			room--
			added = true
		}
		if !added {
			break
		}
	}
	return limits
}

type quickRaceOps struct {
	ctx        context.Context
	connect    func(*Tunnel) error
	entry      func(*Tunnel) error
	stop       func(*Tunnel)
	refresh    func(context.Context) error
	timeout    time.Duration // Per candidate, not a deadline for the whole job.
	retryDelay time.Duration
}

type quickAttempt struct {
	tunnel *Tunnel
	code   string
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}
type quickArrival struct {
	attempt *quickAttempt
	err     error
}
type quickRegionRace struct {
	name                    string
	steps                   []int
	kept, active, attempted int
	lastError               string
	triedHosts, triedIPs    map[string]bool
}

func (m *Manager) promoteQuickCandidate(t *Tunnel) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || t.connectionContext().Err() != nil || t.Status != "up" || !t.EntryReady {
		return false
	}
	t.quickCandidate.Store(false)
	return true
}

// Failed workers are refilled immediately. Exhausted countries refresh and
// retry until their quotas are ready or the user cancels. A completed country
// starts no more candidates. Cleanup never releases a slot before its worker.
func (m *Manager) runQuickRace(job *Job, targets []quickTarget, templateID int, ops quickRaceOps) <-chan struct{} {
	if ops.ctx == nil {
		ops.ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ops.ctx)
	defer cancel()
	if ops.connect == nil {
		ops.connect = m.tryNode
	}
	if ops.entry == nil {
		ops.entry = m.ensureTunnelEntry
	}
	if ops.stop == nil {
		ops.stop = func(t *Tunnel) {
			t.stop()
			if t.EntryReady {
				_ = m.disconnectExitNodes(t)
			}
		}
	}
	if ops.refresh == nil {
		ops.refresh = func(ctx context.Context) error { _, err := m.refreshNodesContext(ctx); return err }
	}
	if ops.timeout <= 0 {
		ops.timeout = 50 * time.Second
	}
	if ops.retryDelay <= 0 {
		ops.retryDelay = reconnectBackoffMin
	}
	regions := map[string]*quickRegionRace{}
	for _, region := range quickRegions {
		regions[region.Code] = &quickRegionRace{name: region.Name, triedHosts: map[string]bool{}, triedIPs: map[string]bool{}}
	}
	for _, target := range targets {
		regions[target.code].steps = append(regions[target.code].steps, target.step)
	}
	limits := m.quickRaceLimits(targets)
	window := 0
	for _, limit := range limits {
		window += limit
	}
	firstOrder := m.reserveOrders(quickSummaryStep - quickExitStep)
	arrivalOrder := 0
	arrivals := make(chan quickArrival, window)
	released := make(chan struct{}, 1)
	live := map[*quickAttempt]bool{}
	var cleanup sync.WaitGroup
	discard := func(a *quickAttempt) {
		if !a.tunnel.quickCandidate.Load() {
			return
		}
		a.once.Do(func() {
			m.mu.Lock()
			a.tunnel.quickRetiring = true
			m.mu.Unlock()
			a.cancel()
			a.tunnel.interruptQuickCandidate()
			cleanup.Add(1)
			go func() {
				defer cleanup.Done()
				<-a.done
				ops.stop(a.tunnel)
				m.mu.Lock()
				if m.tunnels[a.tunnel.Slot] == a.tunnel {
					delete(m.tunnels, a.tunnel.Slot)
				}
				m.mu.Unlock()
				_ = m.saveState()
				select {
				case released <- struct{}{}:
				default:
				}
			}()
		})
	}
	complete := func() bool {
		for _, plan := range regions {
			if plan.kept < len(plan.steps) {
				return false
			}
		}
		return true
	}
	fill := func() map[string]bool {
		queues := map[string][]Node{}
		for _, node := range m.quickRacePicks(targets) {
			code := normalizeRegion(node.CountryCode)
			plan := regions[code]
			if !plan.triedHosts[node.HostName] && (node.IP == "" || !plan.triedIPs[node.IP]) {
				queues[code] = append(queues[code], node)
			}
		}
		for ctx.Err() == nil {
			added := false
			for _, region := range quickRegions {
				code, plan := region.Code, regions[region.Code]
				if ctx.Err() != nil || plan.kept == len(plan.steps) || plan.active+plan.kept >= limits[code] || len(queues[code]) == 0 {
					continue
				}
				m.mu.RLock()
				_, slotErr := m.freeSlot()
				m.mu.RUnlock()
				if slotErr != nil {
					continue
				}
				node := queues[code][0]
				queues[code] = queues[code][1:]
				plan.triedHosts[node.HostName] = true
				if node.IP != "" {
					plan.triedIPs[node.IP] = true
				}
				added = true
				candidateCtx, candidateCancel := context.WithTimeout(ctx, ops.timeout)
				tunnel, err := m.prepareTunnelMode(node, firstOrder, templateID, false, candidateCtx)
				if err != nil {
					candidateCancel()
					plan.lastError = firstLine(err.Error())
					continue
				}
				a := &quickAttempt{tunnel: tunnel, code: code, cancel: candidateCancel, done: make(chan struct{})}
				live[a] = true
				plan.active++
				plan.attempted++
				go func() {
					defer close(a.done)
					interrupt := context.AfterFunc(candidateCtx, tunnel.interruptQuickCandidate)
					defer interrupt()
					err := candidateCtx.Err()
					if err == nil {
						err = ops.connect(tunnel)
					}
					if err == nil {
						err = candidateCtx.Err()
					}
					if err == nil && !m.acceptExitIP(tunnel) {
						err = fmt.Errorf("出口已停止或 IP 与已有出口重复")
					}
					arrivals <- quickArrival{attempt: a, err: err}
				}()
			}
			if !added {
				break
			}
		}
		exhausted := map[string]bool{}
		for _, region := range quickRegions {
			plan := regions[region.Code]
			if plan.kept == len(plan.steps) {
				continue
			}
			detail := fmt.Sprintf("%s本次已补齐 %d / %d 个，已尝试 %d 个候选；后台继续连接…", plan.name, plan.kept, len(plan.steps), plan.attempted)
			if plan.active == 0 && len(queues[region.Code]) == 0 {
				exhausted[region.Code] = true
				detail = fmt.Sprintf("%s候选暂时不足，已尝试 %d 个；等待刷新来源后继续补齐，已连通节点可继续使用。", plan.name, plan.attempted)
			}
			if plan.lastError != "" {
				detail += " 最近提示：" + plan.lastError
			}
			for _, step := range plan.steps[plan.kept:] {
				job.Set(step, "running", detail)
			}
		}
		return exhausted
	}
	ticker := time.NewTicker(ops.retryDelay)
	defer ticker.Stop()
	backoff := ops.retryDelay
	nextRefresh := time.Now().Add(backoff)
	refreshed := make(chan error, 1)
	refreshing := false
	var refreshingCodes map[string]bool
	for !complete() && ctx.Err() == nil {
		exhausted := fill()
		select {
		case result := <-arrivals:
			a := result.attempt
			delete(live, a)
			plan := regions[a.code]
			plan.active--
			if result.err != nil || plan.kept == len(plan.steps) || ctx.Err() != nil {
				if result.err != nil {
					plan.lastError = firstLine(result.err.Error())
				}
				discard(a)
				continue
			}
			step := plan.steps[plan.kept]
			m.mu.Lock()
			a.tunnel.Order = firstOrder + uint64(arrivalOrder)
			m.mu.Unlock()
			if err := ops.entry(a.tunnel); err != nil {
				plan.lastError = firstLine(err.Error())
				discard(a)
				continue
			}
			if !m.promoteQuickCandidate(a.tunnel) {
				plan.lastError = "入口尚未就绪，继续尝试同地区候选"
				discard(a)
				continue
			}
			a.cancel()
			plan.kept++
			arrivalOrder++
			job.Set(step, "ok", a.tunnel.ExitIP)
			m.updateQuickResult(job, templateID)
			invalidateInbounds()
			_ = m.saveState()
			backoff = ops.retryDelay
			nextRefresh = time.Now().Add(backoff)
			if plan.kept == len(plan.steps) {
				for other := range live {
					if other.code == a.code {
						discard(other)
					}
				}
			}
		case <-released:
		case <-m.nodesChanged:
		case <-ticker.C:
			if !refreshing && len(exhausted) > 0 && !time.Now().Before(nextRefresh) {
				refreshing, refreshingCodes = true, exhausted
				go func() { refreshed <- ops.refresh(ctx) }()
			}
		case err := <-refreshed:
			refreshing = false
			for code := range refreshingCodes {
				plan := regions[code]
				plan.triedHosts, plan.triedIPs = map[string]bool{}, map[string]bool{}
				if err != nil {
					plan.lastError = "来源刷新暂不可用，继续重试缓存候选"
				}
			}
			nextRefresh = time.Now().Add(backoff)
			if backoff < reconnectBackoffMax {
				backoff *= 2
				if backoff > reconnectBackoffMax {
					backoff = reconnectBackoffMax
				}
			}
		case <-ctx.Done():
		}
	}
	for a := range live {
		discard(a)
	}
	for _, plan := range regions {
		for _, step := range plan.steps[plan.kept:] {
			job.Set(step, "failed", plan.name+"后台补齐已停止，已保留连通节点。")
		}
	}
	cleaned := make(chan struct{})
	go func() { cleanup.Wait(); close(cleaned) }()
	return cleaned
}
