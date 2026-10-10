package httpapi

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/tunnel"
	"net/http"
	"time"
)

func (a *Server) vpnStatus(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	result := map[string]any{"configured": a.vpnClient != nil, "reachable": false, "mode": "isolated-openvpn", "message": "未显式接入独立VPN执行器"}
	if a.vpnClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		caps, err := a.vpnClient.Capabilities(ctx)
		if err == nil {
			result["reachable"] = true
			result["capabilities"] = caps
			result["message"] = "已验证root执行器身份；启动仍需实际TLS/DNS/出口与内核检查"
		} else {
			result["message"] = "已配置，但无法核实VPN执行器；不会直连或放宽权限"
		}
	}
	write(w, 200, result)
}
func (a *Server) getVPN(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	config, _, err := a.store.VPN(r.PathValue("id"))
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	write(w, 200, config)
}
func (a *Server) saveVPN(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input domain.VPNInput
	if !decode(w, r, &input) {
		return
	}
	if err := input.Validate(); err != nil {
		fail(w, 400, "invalid_vpn_selection", err.Error())
		return
	}
	if err := a.store.SaveVPN(r.PathValue("id"), input); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			a.storeError(w, err)
			return
		}
		if errors.Is(err, tunnel.ErrProfile) {
			fail(w, 400, "vpn_profile_rejected", "配置不符合所选证书/算法/身份策略；请检查配置，不会跳过TLS验证")
			return
		}
		fail(w, 409, "vpn_selection_rejected", "无法保存候选快照：请确认缓存新鲜、官方来源和材料有效，或检查存储状态")
		return
	}
	if err := a.store.SetStatus(r.PathValue("id"), "stopped", "已保存VPN配置快照及名称绑定；需要明确启动后才联网"); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) removeVPN(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.DeleteVPN(r.PathValue("id")); err != nil {
		a.runtimeError(w, err)
		return
	}
	if err := a.store.SetStatus(r.PathValue("id"), "stopped", "VPN绑定已移除，入口凭据保留"); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
