package httpapi

import (
	"context"
	"net/http"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/vpngate"
)

// Identity records are short-lived server-side observations, never a browser
// supplied flag saying a certificate is trusted. Saving rechecks the exact node
// IP/profile snapshot to prevent selecting a refreshed/changed recipient.
func (a *Server) cachedIdentity(node domain.Node) (domain.NodeIdentity, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, ok := a.nodeIdentities[node.ID]
	if !ok || !identity.Verified || identity.VPNConnected || identity.IP != node.IP || identity.ProfileSHA256 != vpngate.ProfileDigest(node.Config) || time.Since(identity.CheckedAt) > 5*time.Minute || time.Until(identity.CheckedAt) > time.Minute || !time.Now().Before(identity.NotAfter) {
		return domain.NodeIdentity{}, false
	}
	return identity, true
}
func (a *Server) nodeIdentity(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	node, err := a.store.NodeForRuntime(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if node.Source != vpngate.Source || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
		fail(w, 409, "untrusted_node_source", "只对官方HTTPS来源的候选验证身份")
		return
	}
	if cached, ok := a.cachedIdentity(node); ok {
		write(w, 200, cached)
		return
	}
	select {
	case a.identitySlot <- struct{}{}:
		defer func() { <-a.identitySlot }()
	default:
		fail(w, 429, "identity_busy", "已有节点身份检测进行中，请稍后再试")
		return
	}
	a.mu.Lock()
	limited := time.Since(a.lastIdentity) < 3*time.Second
	if !limited {
		a.lastIdentity = time.Now()
	}
	a.mu.Unlock()
	if limited {
		fail(w, 429, "identity_rate_limited", "身份检测至少间隔3秒，不批量扫描节点")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	identity, err := a.inspectIdentity(ctx, node)
	if err != nil {
		fail(w, 502, "identity_unverified", "无法用官方域名和系统CA验证此节点TLS身份；可能不支持该TLS监听。不会禁用校验或遍历端口。")
		return
	}
	if !identity.Verified || identity.VPNConnected || identity.NodeID != node.ID || identity.IP != node.IP || identity.Source != vpngate.Source || identity.ProfileSHA256 != vpngate.ProfileDigest(node.Config) {
		fail(w, 502, "identity_unverified", "身份响应不匹配当前节点快照")
		return
	}
	a.mu.Lock()
	if len(a.nodeIdentities) >= 32 {
		var oldest string
		var before time.Time
		for id, value := range a.nodeIdentities {
			if oldest == "" || value.CheckedAt.Before(before) {
				oldest = id
				before = value.CheckedAt
			}
		}
		delete(a.nodeIdentities, oldest)
	}
	a.nodeIdentities[node.ID] = identity
	a.mu.Unlock()
	write(w, 200, identity)
}
