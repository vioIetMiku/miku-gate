package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// Manager 维护所有隧道，负责分配槽位与端口。
type Manager struct {
	mu             sync.RWMutex
	tunnels        map[int]*Tunnel
	nodes          []Node
	fetched        time.Time
	workDir        string
	maxSlots       int
	jobs           JobStore
	inspector      *IPInspector
	latencies      *LatencyInspector
	candidateProbe func(context.Context, Node) candidateCheck
	probeSlots     chan struct{}
	nextOrder      uint64
	entryMu        sync.Mutex
	stateMu        sync.Mutex
	refreshMu      sync.Mutex
	quickMu        sync.Mutex
	quickJob       *Job
	quickCancel    context.CancelFunc
	nodesChanged   chan struct{}
}

func NewManager(maxSlots int, workDir string) *Manager {
	return &Manager{
		tunnels:      map[int]*Tunnel{},
		workDir:      workDir,
		maxSlots:     maxSlots,
		probeSlots:   make(chan struct{}, 12),
		nodesChanged: make(chan struct{}, 1),
	}
}

// RefreshNodes 重新拉取节点列表。
func (m *Manager) RefreshNodes() (int, error) {
	return m.refreshNodesContext(context.Background())
}

func (m *Manager) refreshNodesContext(ctx context.Context) (int, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.RLock()
	recent := time.Since(m.fetched) < 5*time.Second
	count := len(m.nodes)
	m.mu.RUnlock()
	if recent && count > 0 {
		return count, nil
	}
	nodes, err := fetchNodeSourcesProgress(ctx, 60*time.Second, func(partial []Node) {
		m.mu.Lock()
		// Keep the previous pool usable while slower sources are still loading.
		m.nodes = mergeSourceNodes(append([]Node(nil), partial...), m.nodes)
		sort.SliceStable(m.nodes, func(i, j int) bool {
			if a, b := nodePriority(m.nodes[i]), nodePriority(m.nodes[j]); a != b {
				return a < b
			}
			return m.nodes[i].SpeedMbps > m.nodes[j].SpeedMbps
		})
		m.mu.Unlock()
		m.signalNodesChanged()
	})
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.nodes = nodes
	m.fetched = time.Now()
	m.mu.Unlock()
	m.signalNodesChanged()
	return len(nodes), nil
}

func (m *Manager) signalNodesChanged() {
	select {
	case m.nodesChanged <- struct{}{}:
	default:
	}
}

func (m *Manager) Nodes() ([]Node, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, len(m.nodes))
	copy(out, m.nodes)
	return out, m.fetched
}

func (m *Manager) Tunnels() []*Tunnel {
	return m.tunnelList(false)
}

func (m *Manager) allTunnels() []*Tunnel {
	return m.tunnelList(true)
}

func (m *Manager) tunnelList(includeCandidates bool) []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		if includeCandidates || !t.quickCandidate.Load() {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Order, out[j].Order
		if a == 0 {
			a = uint64(out[i].Slot)
		}
		if b == 0 {
			b = uint64(out[j].Slot)
		}
		if a == b {
			return out[i].Slot < out[j].Slot
		}
		return a < b
	})
	return out
}

// Speculative quick-setup connections reserve endpoints and slots, but only
// selected exits belong in the UI, subscriptions and restart state.
func (m *Manager) visibleTunnels() []*Tunnel {
	return m.Tunnels()
}

// freeSlot 找一个未占用的槽位。槽位同时决定端口与网段。
func (m *Manager) freeSlot() (int, error) {
	capacity := linkSubnetCapacity
	if m.maxSlots > 0 && m.maxSlots < capacity {
		capacity = m.maxSlots
	}
	for i := 1; i <= capacity; i++ {
		if _, used := m.tunnels[i]; !used {
			return i, nil
		}
	}
	if m.maxSlots > 0 {
		return 0, fmt.Errorf("已达到显式设置的 -max %d", m.maxSlots)
	}
	return 0, fmt.Errorf("本实例可用网络地址空间已用尽")
}

// Start 为指定节点开一条隧道，返回分配到的本地端口。
func (m *Manager) Start(node Node) (*Tunnel, error) {
	return m.startOrdered(node, 0)
}

func (m *Manager) reserveOrders(count int) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tunnels {
		order := t.Order
		if order == 0 {
			order = uint64(t.Slot)
		}
		if order > m.nextOrder {
			m.nextOrder = order
		}
	}
	first := m.nextOrder + 1
	m.nextOrder += uint64(count)
	return first
}

func (m *Manager) startOrdered(node Node, order uint64) (*Tunnel, error) {
	return m.prepareTunnel(node, order, 0, true)
}

func (m *Manager) prepareTunnel(node Node, order uint64, templateID int, launch bool) (*Tunnel, error) {
	return m.prepareTunnelMode(node, order, templateID, launch, nil)
}

func (m *Manager) prepareTunnelMode(node Node, order uint64, templateID int, launch bool, quickContext context.Context) (*Tunnel, error) {
	m.mu.Lock()
	for _, other := range m.tunnels {
		if sameNodeEndpoint(node, other.Node) {
			m.mu.Unlock()
			return nil, fmt.Errorf("此节点已被另一出口使用，请选择同国家的其他节点")
		}
	}
	slot, err := m.freeSlot()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	// 端口随机取，避免固定规律撞上机器上的其他服务
	taken := map[int]bool{}
	for _, other := range m.tunnels {
		taken[other.Port] = true
	}
	port, err := freeRandomPort(taken)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	cred, err := newSocksCred()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if order == 0 {
		for _, other := range m.tunnels {
			old := other.Order
			if old == 0 {
				old = uint64(other.Slot)
			}
			if old > m.nextOrder {
				m.nextOrder = old
			}
		}
		m.nextOrder++
		order = m.nextOrder
	}
	var quickCancel context.CancelFunc
	if quickContext != nil {
		quickContext, quickCancel = context.WithCancel(quickContext)
	}
	t := &Tunnel{
		Slot:            slot,
		Port:            port,
		Node:            node,
		Status:          "starting",
		Since:           time.Now(),
		Cred:            cred,
		PreferredRegion: normalizeRegion(node.CountryCode),
		Order:           order,
		TemplateID:      templateID,
		EntryReady:      templateID == 0,
		quickContext:    quickContext,
		quickCancel:     quickCancel,
	}
	t.quickCandidate.Store(quickContext != nil)
	m.tunnels[slot] = t
	m.mu.Unlock()

	if launch {
		go m.bringUp(t, true)
	}
	return t, nil
}

// bringUp 把一条隧道拉起来。
//
// notify 决定成功后是否立刻重建后端配置。换节点重连时要传 false：
// 那条路径随后会调 rebind/resync 把入站改绑到新节点，在那之前重建配置
// 会因为入站还指着旧节点名而把路由规则丢掉。
func (m *Manager) bringUp(t *Tunnel, notify bool) {
	m.bringUpPersist(t, notify, true)
}

// 自动重连的退避区间：一轮候选全挂后等一会儿再刷新节点列表重来，
// 别把死节点列表打爆，也别让恢复拖太久。
const (
	reconnectBackoffMin = 5 * time.Second
	reconnectBackoffMax = 60 * time.Second
)

// bringUpPersist 把一条隧道拉起来。
//
// persist=false：单轮调用，全失败就标 failed。
// persist=true（新建 / 自动重连 / 重启恢复）：一轮全失败不放弃，退避后刷新节点列表再来一轮，
// 一直循环到连上或这条隧道被用户停掉。VPN Gate 死节点多，"当前都不可用"往往只是
// 这一批候选恰好都挂了，过一会儿就有新节点，不该让出口永久躺死。
func (m *Manager) bringUpPersist(t *Tunnel, notify bool, persist bool) {
	backoff := reconnectBackoffMin
	for {
		if m.tryCandidates(t, notify) {
			return
		}
		// 隧道已被用户停掉或从管理器移除，别再重试
		if !persist || !m.tunnelActive(t) {
			if persist {
				return
			}
			t.Status = "failed"
			if serr := m.saveState(); serr != nil {
				log.Printf("保存状态失败: %v", serr)
			}
			return
		}

		t.Status = "starting"
		t.Err = fmt.Sprintf("%s 暂无可用节点，%.0f 秒后自动重试同国家节点", t.preferredRegion(), backoff.Seconds())
		log.Printf("隧道 %d 一轮候选均失败，%.0f 秒后刷新节点重试", t.Slot, backoff.Seconds())
		time.Sleep(backoff)
		if !m.tunnelActive(t) {
			return
		}
		if _, err := m.RefreshNodes(); err != nil {
			log.Printf("重试前刷新节点列表失败: %v", err)
		}
		if backoff < reconnectBackoffMax {
			backoff *= 2
			if backoff > reconnectBackoffMax {
				backoff = reconnectBackoffMax
			}
		}
	}
}

// tryCandidates 走一轮候选节点，成功返回 true。失败不改 Status（留给调用方决定）。
func (m *Manager) tryCandidates(t *Tunnel, notify bool) bool {
	// VPN Gate 是志愿者节点，列表里有相当比例已下线或满员（AUTH_FAILED），
	// 连不上就顺着候选列表换下一个，不必让用户手动试。
	candidates := m.candidatesFor(t)
	if m.latencies != nil || m.candidateProbe != nil {
		t.Err = "正在预检同国家候选并匹配ASN属地"
		candidates = m.preflightCandidates(t, candidates)
	}
	for i, node := range candidates {
		if !m.tunnelActive(t) {
			return false
		}
		// 其他隧道可能在重试期间占用了这个节点，跳过以免多个端口撞同一出口 IP
		if !m.claimCandidate(t, node) {
			continue
		}
		t.Status = "starting"
		if i > 0 {
			t.Err = fmt.Sprintf("已换到第 %d 个候选节点", i+1)
		}

		err := m.tryNode(t)
		if !m.tunnelActive(t) {
			t.stop()
			return false
		}
		if err == nil {
			if !m.acceptExitIP(t) {
				err = fmt.Errorf("出口IP已被另一连接使用，继续尝试同国家节点")
			}
		}
		if err == nil {
			t.forgetFailedCandidate(node.HostName)
			t.Err = ""
			if old := t.prevHostOf(); old != "" {
				if err := m.rebind(old, t); err == nil {
					t.setPrevHost("")
				} else {
					t.EntryError = err.Error()
				}
			}
			if err := m.ensureTunnelEntry(t); err != nil {
				t.EntryError = err.Error()
			}
			if serr := m.saveState(); serr != nil {
				log.Printf("保存状态失败: %v", serr)
			}
			if notify {
				m.notifyPanel()
			}
			return true
		}
		t.Err = err.Error()
		if !errors.Is(err, errExitIdentityPending) {
			t.rememberFailedCandidate(node.HostName)
		}
		t.mu.Lock()
		if t.ovpn != nil && t.ovpn.Process != nil {
			_ = t.ovpn.Process.Kill()
			t.ovpn = nil
		}
		t.mu.Unlock()
		t.teardownNetns()
		t.ExitIP = ""
	}
	return false
}

// tunnelActive 判断这条隧道是否还归管理器所有且未被用户停掉。
// 用指针比对：Stop 会从 map 里删除并把 Status 置 stopped，
// 重连循环据此退出，避免对着一条已经不存在的隧道空转。
func (m *Manager) tunnelActive(t *Tunnel) bool {
	if t.halted.Load() || t.connectionContext().Err() != nil {
		return false
	}
	m.mu.RLock()
	cur, ok := m.tunnels[t.Slot]
	m.mu.RUnlock()
	return ok && cur == t
}

// tryNode 尝试用当前节点把隧道拉起来。
func (m *Manager) tryNode(t *Tunnel) error {
	ctx := t.connectionContext()
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	t.lastProbeSample = nil
	t.mu.Unlock()
	node, err := resolveNodeProfileContext(ctx, t.Node, m.workDir)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	t.Node = node
	m.mu.Unlock()
	if t.quickCandidate.Load() {
		if err := m.quickEndpointPreflight(ctx, node); err != nil {
			return err
		}
	}
	if err := t.setupNetns(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.startOpenVPN(m.workDir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.listener == nil {
		if err := t.serve(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ip, geo, err := t.probeExitDetails()
	if err != nil {
		return err
	}
	t.ExitIP = ip
	if geo != nil {
		if code := countryCodeFromLocation(geo.Location); code != "" && t.preferredRegion() != "" && code != t.preferredRegion() {
			return fmt.Errorf("实际出口位于 %s，继续寻找 %s 节点", countryCN[code], countryCN[t.preferredRegion()])
		}
	}
	if m.inspector != nil && geo != nil {
		m.inspector.seedGeo(*geo)
	}
	if err := m.validateExitCountry(t, geo); err != nil {
		return err
	}
	if m.latencies != nil {
		t.mu.Lock()
		sample := t.lastProbeSample
		t.mu.Unlock()
		if sample != nil && sample.MS != nil {
			m.latencies.recordTunnelSample(node, *sample.MS, sample.Target)
		}
	}
	return nil
}

// candidatesFor 只选择原国家，避开最近失败的备选。
//
// 打头的一定是 t.Node：自动重连的目标是把这条出口恢复原样，先试原节点。
// 备选会避开用户手动换掉过的节点——那些是他明确不想要的 IP，
// 让重连悄悄换回去等于撤销了他的操作。
func (m *Manager) candidatesFor(t *Tunnel) []Node { return m.regionalCandidates(t) }

// Stop 停掉一条隧道并释放槽位。
func (m *Manager) Stop(slot int) error {
	invalidateInbounds()
	m.mu.Lock()
	t, ok := m.tunnels[slot]
	candidate := ok && t.quickCandidate.Load()
	if candidate {
		t.quickCancel()
	}
	if ok && !candidate {
		delete(m.tunnels, slot)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("槽位 %d 没有运行中的隧道", slot)
	}
	if candidate {
		t.interruptQuickCandidate()
		return nil
	}
	t.stop()
	if err := m.disconnectExitNodes(t); err != nil {
		log.Printf("出口已停止，停用关联入口失败: %v", err)
	}
	if err := m.saveState(); err != nil {
		log.Printf("保存状态失败: %v", err)
	}
	m.notifyPanel()
	return nil
}

// Swap 把一条隧道换到同地区的另一个节点上，端口与已分发的客户端配置保持不变。
//
// 与健康检查的自动重连不同：那边优先重连原节点（目标是恢复），
// 这里用户是嫌当前出口 IP 不好用，必须真的换一个。
//
// 换过的节点会记进历史一并避开。只排除"当前这个"是不够的：
// A 换成 B 之后 A 就空出来了，而候选是按速度排的，A 往往又排在最前面，
// 于是再点一次就换回了 A。历史让每次点击都真的换一个没用过的。
func (m *Manager) Swap(slot int) error {
	m.mu.RLock()
	t, ok := m.tunnels[slot]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("槽位 %d 没有运行中的隧道", slot)
	}
	if t.Status == "starting" {
		return fmt.Errorf("这个出口正在连接中，稍等一下")
	}

	node, err := m.pickSwapTarget(t)
	if err != nil {
		return err
	}
	oldHost := t.Node.HostName
	if !m.claimCandidate(t, node) {
		return fmt.Errorf("此节点刚被另一出口使用，请重试")
	}
	m.reconnect(t, oldHost)
	return nil
}

// pickSwapTarget 给"换节点"挑下一个目标，并把该跳过的节点记进历史。
//
// 记两个：换下来的那个，以及刚挑中的这个。挑中的也记是因为真机上踩到过——
// 它连不上时会被候选列表换成别人，但它自己没进历史，
// 于是下次点换节点又从它开始试一遍，白等一轮握手超时。
func (m *Manager) pickSwapTarget(t *Tunnel) (Node, error) {
	avoid := t.swapAvoid()
	picks, err := m.pickNodes(t.preferredRegion(), 1, avoid)
	if err != nil && len(avoid) > 1 {
		// 这个地区的节点都换过一轮了。清掉历史重新开始，
		// 总比告诉用户"没得换了"好——转一圈之后原来那些节点未必还是当初的状态。
		t.forgetSwaps()
		picks, err = m.pickNodes(t.preferredRegion(), 1, t.swapAvoid())
	}
	if err != nil {
		return Node{}, err
	}
	t.rememberSwap(t.Node.HostName)
	t.rememberSwap(picks[0].HostName)
	return picks[0], nil
}

// StopAll 停掉所有隧道并清空状态文件。
func (m *Manager) StopAll() {
	m.cancelQuickSetup()
	for _, t := range m.allTunnels() {
		_ = m.Stop(t.Slot)
	}
}

// SetCred 改一条出口的 SOCKS5 凭据。cred 两个字段都为空表示随机重置。
//
// 改完要通知后端：本机 Xray 的 socks 出站里带着这套凭据，
// 不同步的话面板侧的节点会立刻连不上自己的出口。
func (m *Manager) SetCred(slot int, cred SocksCred) (SocksCred, error) {
	m.mu.RLock()
	t, ok := m.tunnels[slot]
	m.mu.RUnlock()
	if !ok {
		return SocksCred{}, fmt.Errorf("槽位 %d 没有运行中的隧道", slot)
	}

	if cred.User == "" && cred.Pass == "" {
		gen, err := newSocksCred()
		if err != nil {
			return SocksCred{}, err
		}
		cred = gen
	}
	if err := validateCred(cred); err != nil {
		return SocksCred{}, err
	}

	t.setCredential(cred)
	if err := m.saveState(); err != nil {
		log.Printf("保存状态失败: %v", err)
	}
	m.syncCred(t)
	return cred, nil
}

// ReconcileOutbounds 在启动恢复隧道后跑一次，把后端出站对齐到当前隧道（含 SOCKS5 凭据）。
//
// 只为 3x-ui 模式而生：它的 OnTunnelsChanged 是空操作，重启不会重写面板出站，
// 而从旧版本升上来时面板里持久化的 socks 出站没有认证字段，端口一旦要认证就连不上。
// 自建模式恢复时每条隧道 up 都会重建配置，本就自洽，这里跳过免得多重启一次 Xray。
func (m *Manager) ReconcileOutbounds() {
	p, err := openPanel()
	if err != nil || p.Kind() != "3x-ui" {
		return
	}

	// 等隧道尽量都起完再重写一次，避免只覆盖到先 up 的那几条
	deadline := time.Now().Add(90 * time.Second)
	for {
		tunnels := m.Tunnels()
		if len(tunnels) == 0 {
			return
		}
		var up *Tunnel
		settled := true
		for _, t := range tunnels {
			if t.Status == "up" && up == nil {
				up = t
			}
			if t.Status == "starting" {
				settled = false
			}
		}
		if (settled || time.Now().After(deadline)) && up != nil {
			if err := m.resync(up); err != nil {
				log.Printf("启动对账面板出站失败: %v", err)
			}
			return
		}
		if settled || time.Now().After(deadline) {
			return // 全 failed，没有可写的出站
		}
		time.Sleep(2 * time.Second)
	}
}

// syncCred 把新凭据写进后端的 socks 出站。
//
// 两种后端的做法不同：自建模式整份重建配置，3x-ui 模式只改出站那一段。
// 都走 ResyncOutbound，接口语义正好是"重写这条隧道对应的出站"。
func (m *Manager) syncCred(t *Tunnel) {
	if err := m.resync(t); err != nil {
		log.Printf("同步 SOCKS5 凭据到节点链接后端失败: %v", err)
	}
}

// Shutdown 停掉运行态但保留状态文件，让下次启动能恢复同样的隧道。
func (m *Manager) Shutdown() {
	m.cancelQuickSetup()
	for _, t := range m.allTunnels() {
		if t.quickCandidate.Load() {
			t.quickCancel()
		}
		t.stop()
	}
}

// prepareHost 打开转发开关。netns 出网依赖它。
func prepareHost() error {
	if err := cmdRun(exec.Command("sysctl", "-qw", "net.ipv4.ip_forward=1")); err != nil {
		return fmt.Errorf("开启 ip_forward 失败: %w", err)
	}
	return nil
}

// nodeInUse 判断某节点是否已被别的隧道占用。
func (m *Manager) nodeInUse(host string, exceptSlot int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for slot, t := range m.tunnels {
		if slot != exceptSlot && t.Node.HostName == host {
			return true
		}
	}
	return false
}

// rebind 在隧道换节点后，把原先指向旧节点的 3x-ui 入站改绑到新节点。
// 面板不可用时静默跳过，健康检查本身不应因此失败。
func (m *Manager) rebind(oldHost string, t *Tunnel) error {
	x, err := openPanel()
	if err != nil {
		return nil
	}
	return x.Rebind(oldHost, t, m.Tunnels())
}

// resync 在节点没换但重连过之后，把 3x-ui 的出站配置刷新一遍。
// 面板不可用时静默跳过，健康检查本身不应因此失败。
func (m *Manager) resync(t *Tunnel) error {
	x, err := openPanel()
	if err != nil {
		return nil
	}
	return x.ResyncOutbound(t, m.Tunnels())
}

// notifyPanel 告诉后端隧道集合变了。
//
// 自建模式下出站是由隧道列表现算出来的，不通知的话新开的出口在 Xray 里
// 没有对应的 socks 出站，绑定会指向一个不存在的 tag。接管 3x-ui 时是空操作。
// 后端不可用不该让开关出口失败，所以只记日志。
func (m *Manager) notifyPanel() {
	p, err := openPanel()
	if err != nil {
		return
	}
	if err := p.OnTunnelsChanged(m.Tunnels()); err != nil {
		log.Printf("同步节点链接后端失败: %v", err)
	}
}
