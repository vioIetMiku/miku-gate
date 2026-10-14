package httpapi

import (
	"context"
	"net/http"
	"time"

	"mikugate/internal/certassets"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
)

func (a *Server) ingressTemplates(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	write(w, 200, ingress.Templates())
}
func (a *Server) ingressPreview(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input ingress.ManagedRequest
	if !decode(w, r, &input) {
		return
	}
	plan, e := ingress.ManagedPlanFor(input)
	if e != nil {
		fail(w, 400, "ingress_invalid", e.Error())
		return
	}
	write(w, 200, plan)
}
func (a *Server) realityCandidates(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	write(w, 200, ingress.RealityCandidates())
}
func (a *Server) assetGate(w http.ResponseWriter) bool {
	select {
	case a.assetSlot <- struct{}{}:
	default:
		fail(w, 429, "asset_busy", "证书/目标检测正在进行")
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.lastAsset.IsZero() && time.Since(a.lastAsset) < 3*time.Second {
		<-a.assetSlot
		fail(w, 429, "asset_rate_limited", "请稍后再检测")
		return false
	}
	a.lastAsset = time.Now()
	return true
}
func (a *Server) realityProbe(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Hostname       string `json:"hostname"`
		ConfirmNetwork bool   `json:"confirm_network"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.ConfirmNetwork {
		fail(w, 400, "confirm_network_required", "需明确确认VPS主机网络的单目标443TLS探测，不是VPN出口检测")
		return
	}
	host, e := certassets.Hostname(input.Hostname)
	if e != nil {
		fail(w, 400, "target_invalid", "请输入规范的公开域名，不接受URL/IP/端口")
		return
	}
	if !a.assetGate(w) {
		return
	}
	defer func() { <-a.assetSlot }()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	checked, e := a.probeReality(ctx, host)
	if e != nil {
		fail(w, 422, "target_unverified", ingress.ErrReality.Error())
		return
	}
	if checked.Hostname != host || !checked.Suitable || checked.Port != 443 {
		fail(w, 502, "target_invalid", "目标检测状态不匹配")
		return
	}
	write(w, 200, checked)
}
func (a *Server) certificateDiscover(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Hostname    string `json:"hostname"`
		ConfirmRead bool   `json:"confirm_read"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.ConfirmRead {
		fail(w, 400, "confirm_read_required", "需明确确认自动查找域名证书；私钥不回显")
		return
	}
	host, e := certassets.Hostname(input.Hostname)
	if e != nil {
		fail(w, 400, "hostname_invalid", "域名无效")
		return
	}
	if !a.assetGate(w) {
		return
	}
	defer func() { <-a.assetSlot }()
	report, _, e := a.discoverCertificate(host)
	if e != nil || report.Hostname != host || report.PrivateKeyReturned {
		fail(w, 422, "certificate_unavailable", "自动证书检测失败，未返回私钥")
		return
	}
	write(w, 200, report)
}
