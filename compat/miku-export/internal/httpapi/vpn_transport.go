package httpapi

import (
	"mikugate/internal/domain"
	"net/http"
)

func (a *Server) vpnTransportGet(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	c, ok, err := a.store.VPN(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	view := domain.UpstreamView{}
	if ok && c.FrontProxy != nil {
		view = c.FrontProxy.View(c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	write(w, 200, map[string]any{"proxy": view, "configured_vpn": ok, "tcp_only": true, "business_direct_fallback": false})
}
func (a *Server) vpnTransportSave(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		domain.UpstreamInput
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "需明确授权VPN TCP底层前置通道；与最终出口代理分开")
		return
	}
	if err := a.store.SaveVPNTransport(r.PathValue("id"), &in.UpstreamInput); err != nil {
		fail(w, 400, "transport_invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}
func (a *Server) vpnTransportRemove(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.SaveVPNTransport(r.PathValue("id"), nil); err != nil {
		fail(w, 400, "transport_invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}
