package main

import (
	"sort"
	"sync"
	"time"
)

// ExitInbound 是挂在某个出口上的一个 3x-ui 入站。
type ExitInbound struct {
	Stack    string `json:"stack"`
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Remark   string `json:"remark"`
	Protocol string `json:"protocol"`
	Enable   bool   `json:"enable"`
	Tag      string `json:"tag"`
}

// Exit 是界面上的一行：一条隧道加上挂在它出口的所有入站。
// 用户脑子里的单位是"一个出口"，不是"一条隧道"和"一个入站"两样东西。
type Exit struct {
	IPInfo      *IPInfo          `json:"ip_info,omitempty"`
	GateLatency *GateLatency     `json:"gate_latency,omitempty"`
	Stability   *StabilityNotice `json:"stability,omitempty"`
	Slot        int              `json:"slot"`
	Order       uint64           `json:"order,omitempty"`
	Port        int              `json:"port"` // SOCKS5 端口
	Host        string           `json:"host"`
	Region      string           `json:"region"`
	Country     string           `json:"country"`
	ExitIP      string           `json:"exit_ip"`
	Status      string           `json:"status"`
	Err         string           `json:"err,omitempty"`
	Since       time.Time        `json:"since"`
	NeedsNode   bool             `json:"needs_node"`
	EntryError  string           `json:"entry_error,omitempty"`
	// SOCKS5 凭据：界面要能看、能复制、能改
	SocksUser string        `json:"socks_user"`
	SocksPass string        `json:"socks_pass"`
	Inbounds  []ExitInbound `json:"inbounds"`
}

// ExitsView 是主界面需要的全部数据。
type ExitsView struct {
	Imported []ImportedLink `json:"imported,omitempty"`
	Exits    []Exit         `json:"exits"`
	// Direct 是没绑到任何出口的入站，仍然要能看见，否则用户会以为它们不见了
	Direct []ExitInbound `json:"direct"`
	Panel  string        `json:"panel"` // 面板不可用时的原因，空表示正常
	// Backend 是 "3x-ui" 或 "native"。界面据此决定是否提供新建入站入口：
	// 接管面板时入站归面板管，自建模式才由 fanout 自己建。
	Backend string `json:"backend"`
	// PanelInfo 是后端的一行说明，显示在标题旁
	PanelInfo string `json:"panel_info"`
	// PublicIP 是母机公网 IPv4，前端用它当 SOCKS5/分享链接的连接地址
	PublicIP string `json:"public_ip"`
}

// inboundCache 给入站列表做很短的缓存。界面每几秒轮询一次，
// 而每次读入站都要顺带解析一遍完整的 Xray 配置，没必要每次都真的去问面板。
type inboundCache struct {
	mu   sync.Mutex
	at   time.Time
	list []Inbound
	err  error
}

const inboundCacheTTL = 2500 * time.Millisecond

var ibCache inboundCache

func cachedInbounds(live map[string]bool) ([]Inbound, error) {
	ibCache.mu.Lock()
	defer ibCache.mu.Unlock()
	if time.Since(ibCache.at) < inboundCacheTTL {
		return ibCache.list, ibCache.err
	}

	var list []Inbound
	x, err := openPanel()
	if err == nil {
		list, err = x.Inbounds(live)
	}
	ibCache.at, ibCache.list, ibCache.err = time.Now(), list, err
	return list, err
}

// invalidateInbounds 在写操作之后调用，让下一次读立刻反映改动。
func invalidateInbounds() {
	ibCache.mu.Lock()
	ibCache.at = time.Time{}
	ibCache.mu.Unlock()
}

// ExitsOf 把隧道和入站 join 成界面直接可用的形态。
func (m *Manager) ExitsOf() ExitsView {
	tunnels := m.visibleTunnels()
	view := ExitsView{Exits: make([]Exit, 0, len(tunnels)), PublicIP: publishingHost()}
	view.Imported = m.verifiedImportedLinks()

	// 先填后端类型：入站读取失败时界面仍要知道当前是哪种模式
	if p, err := openPanel(); err == nil {
		view.Backend = p.Kind()
		view.PanelInfo = p.Describe()
	}

	live := map[string]bool{}
	for _, t := range tunnels {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}

	byHost := map[string]int{}
	for i, t := range tunnels {
		byHost[sanitizeTag(t.Node.HostName)] = i
		cred := t.credential()
		var info *IPInfo
		var latency *GateLatency
		if m.inspector != nil && t.ExitIP != "" {
			m.inspector.enqueue(t.ExitIP)
			info = m.inspector.get(t.ExitIP)
		}
		if m.latencies != nil && t.Status == "up" {
			latency = m.latencies.get(t.Node)
		}
		view.Exits = append(view.Exits, Exit{
			Slot: t.Slot, Port: t.Port, Host: t.Node.HostName,
			Order:  t.Order,
			Region: t.Node.CountryCode, Country: nodeLabel(t.Node),
			ExitIP: t.ExitIP, Status: t.Status, Err: t.Err, Since: t.Since,
			NeedsNode: t.TemplateID != 0, EntryError: t.EntryError,
			SocksUser: cred.User, SocksPass: cred.Pass,
			IPInfo:      info,
			GateLatency: latency,
			Stability:   t.stabilityNotice(time.Now()),
		})
	}

	list, err := cachedInbounds(live)
	if err != nil {
		view.Panel = err.Error()
		return view
	}
	clientHosts := map[int][]string{}
	nativeIDs := map[int]bool{}
	directClients := map[int]bool{}
	if panel, err := openPanel(); err == nil {
		if native, ok := panel.(*Native); ok {
			native.mu.Lock()
			for _, inbound := range native.store.Inbounds {
				nativeIDs[inbound.ID] = true
				for _, client := range inbound.Clients {
					if inbound.Enable && client.Enable {
						host := client.BoundTo
						if host == "" {
							host = inbound.BoundTo
						}
						if host == "" {
							directClients[inbound.ID] = true
						} else {
							clientHosts[inbound.ID] = append(clientHosts[inbound.ID], host)
						}
					}
				}
				// Empty entrances may still be used as templates.
				if len(inbound.Clients) == 0 && inbound.BoundTo == "" {
					directClients[inbound.ID] = true
				}
			}
			native.mu.Unlock()
		}
	}

	for _, ib := range list {
		// Legacy duplicates remain in storage for recovery, not as usable ports.
		if !ib.Enable {
			continue
		}
		row := ExitInbound{
			Stack: ib.Stack,
			ID:    ib.ID, Port: ib.Port, Remark: ib.Remark,
			Protocol: ib.Protocol, Enable: ib.Enable, Tag: ib.Tag,
		}
		attached := map[int]bool{}
		hosts := clientHosts[ib.ID]
		if !nativeIDs[ib.ID] {
			hosts = []string{ib.BoundTo}
		}
		for _, host := range hosts {
			if i, ok := byHost[host]; host != "" && ok && !attached[i] {
				view.Exits[i].Inbounds = append(view.Exits[i].Inbounds, row)
				attached[i] = true
			}
		}
		// A disconnected binding is not a direct entrance or a reusable template.
		if nativeIDs[ib.ID] && directClients[ib.ID] || !nativeIDs[ib.ID] && ib.BoundTo == "" {
			view.Direct = append(view.Direct, row)
		}
	}
	sort.SliceStable(view.Direct, func(i, j int) bool { return view.Direct[i].ID < view.Direct[j].ID })
	for i := range view.Exits {
		sort.SliceStable(view.Exits[i].Inbounds, func(a, b int) bool { return view.Exits[i].Inbounds[a].ID < view.Exits[i].Inbounds[b].ID })
	}
	return view
}
