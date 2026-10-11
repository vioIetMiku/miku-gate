package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"time"

	"mikugate/internal/certassets"
	"mikugate/internal/domain"
	"mikugate/internal/egress"
	"mikugate/internal/vpngate"
)

type intelligenceConsent struct {
	Confirm bool `json:"confirm"`
}

func (a *Server) intelligenceGate(w http.ResponseWriter, r *http.Request) bool {
	var consent intelligenceConsent
	if !decode(w, r, &consent) {
		return false
	}
	if !consent.Confirm {
		fail(w, 400, "confirm_required", "查询会向固定第三方发送目标IP，并走宿主HTTPS；请明确同意")
		return false
	}
	return true
}
func (a *Server) intelligenceLookup(w http.ResponseWriter, r *http.Request, ip string, current func() bool) {
	select {
	case a.intelligenceSlot <- struct{}{}:
		defer func() { <-a.intelligenceSlot }()
	default:
		fail(w, 429, "intelligence_busy", "已有归属/信誉查询进行中")
		return
	}
	a.mu.Lock()
	limited := time.Since(a.lastIntelligence) < 3*time.Second
	if !limited {
		a.lastIntelligence = time.Now()
	}
	a.mu.Unlock()
	if limited {
		fail(w, 429, "intelligence_rate_limited", "查询至少间隔3秒；短缓存保留来源时间，不批量扫描")
		return
	}
	report, err := a.intelligenceLookupFunc(r.Context(), ip)
	if err != nil || report.IP != ip {
		fail(w, 502, "intelligence_unavailable", "归属查询未完成；不会将缺数据标成纯净或住宅")
		return
	}
	if current != nil && !current() {
		fail(w, 409, "intelligence_target_changed", "查询期间节点/运行出口已经变化，请重新检查；不把旧IP当当前出口")
		return
	}
	write(w, 200, report)
}
func (a *Server) nodeIntelligence(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !a.intelligenceGate(w, r) {
		return
	}
	node, err := a.store.NodeForRuntime(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if node.Source != vpngate.Source || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute || egress.PublicIPv4(node.IP) != nil {
		fail(w, 409, "untrusted_node_source", "只查询当前官方缓存候选，不接受浏览器伪造节点IP")
		return
	}
	a.intelligenceLookup(w, r, node.IP, func() bool {
		actual, err := a.store.NodeForRuntime(node.ID)
		return err == nil && actual.Source == vpngate.Source && actual.IP == node.IP && actual.FetchedAt.Equal(node.FetchedAt)
	})
}
func (a *Server) exitIntelligence(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !a.intelligenceGate(w, r) {
		return
	}
	if _, _, err := a.store.ExitForRuntime(r.PathValue("id")); err != nil {
		a.storeError(w, err)
		return
	}
	if a.gateway == nil {
		fail(w, 409, "exit_ip_unmeasured", "没有已测量且健康的运行出口IP")
		return
	}
	state, ok := a.gateway.Snapshot(r.PathValue("id"))
	if !ok || !state.Healthy || !state.ProxyListening || state.Proof.ExitIP == "" || time.Since(state.CheckedAt) > 5*time.Minute || egress.PublicIPv4(state.Proof.ExitIP) != nil {
		fail(w, 409, "exit_ip_unmeasured", "先启动并探测实际出口，不使用配置节点IP冒充实测公网出口")
		return
	}
	a.intelligenceLookup(w, r, state.Proof.ExitIP, func() bool {
		current, ok := a.gateway.Snapshot(state.ExitID)
		return ok && current.Healthy && current.ProxyListening && current.Proof.ExitIP == state.Proof.ExitIP && current.StartedAt == state.StartedAt
	})
}
func (a *Server) domainIntelligence(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		Hostname string `json:"hostname"`
		Confirm  bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	host, err := certassets.Hostname(in.Hostname)
	if !in.Confirm || err != nil || !domain.PublicHostname(host) {
		fail(w, 400, "domain_lookup_invalid", "需规范公开域名和明确同意DNS/第三方归属查询；不接受URL/私网名称")
		return
	}
	select {
	case a.intelligenceSlot <- struct{}{}:
		defer func() { <-a.intelligenceSlot }()
	default:
		fail(w, 429, "intelligence_busy", "已有归属查询进行中")
		return
	}
	a.mu.Lock()
	limited := time.Since(a.lastIntelligence) < 3*time.Second
	if !limited {
		a.lastIntelligence = time.Now()
	}
	a.mu.Unlock()
	if limited {
		fail(w, 429, "intelligence_rate_limited", "查询至少间隔3秒")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	addresses, err := a.resolveIntelligenceDomain(ctx, host)
	if err != nil || len(addresses) == 0 || len(addresses) > 16 {
		fail(w, 502, "domain_lookup_failed", "域名公共IPv4解析不可用")
		return
	}
	list := []string{}
	for _, address := range addresses {
		if !address.Is4() || egress.PublicIPv4(address.String()) != nil {
			fail(w, 400, "domain_lookup_private", "解析含非公共地址；不查询或连接私网目标")
			return
		}
		list = append(list, address.String())
	}
	sort.Strings(list)
	report, err := a.intelligenceLookupFunc(ctx, list[0])
	if err != nil || report.IP != list[0] {
		fail(w, 502, "intelligence_unavailable", "归属数据不可用")
		return
	}
	write(w, 200, map[string]any{"hostname": host, "resolved_addresses": list, "selected_ip": list[0], "report": report, "warning": "域名可因CDN/Anycast地区变化；仅首个排序地址，IP归属不是网站经营者所在地或所有权证明。未连接用户域名端口。"})
}
func defaultDomainResolver(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
}
