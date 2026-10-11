package httpapi

import (
	"errors"
	"net/http"

	"mikugate/internal/certassets"
	"mikugate/internal/coreruntime"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
)

func (a *Server) coreError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, coreruntime.ErrBusy):
		fail(w, 409, "ingress_busy", e.Error())
	case errors.Is(e, coreruntime.ErrUnavailable):
		fail(w, 503, "core_unavailable", coreruntime.ErrUnavailable.Error())
	case errors.Is(e, coreruntime.ErrCore):
		fail(w, 422, "core_unverified", coreruntime.ErrCore.Error())
	case errors.Is(e, coreruntime.ErrExit):
		fail(w, 409, "exit_unavailable", coreruntime.ErrExit.Error())
	case errors.Is(e, certassets.ErrCertificate) || errors.Is(e, ingress.ErrReality):
		fail(w, 422, "asset_expired", "保存的证书/REALITY目标已无效或过期，请停止后重新验证并保存")
	default:
		a.runtimeError(w, e)
	}
}
func (a *Server) managedGet(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	view, e := a.store.ManagedView(r.PathValue("id"))
	if e != nil {
		a.runtimeError(w, e)
		return
	}
	write(w, 200, view)
}
func (a *Server) managedRuntime(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	id := r.PathValue("id")
	if !a.requireExit(w, id) {
		return
	}
	if a.cores != nil {
		if state, ok := a.cores.Snapshot(id); ok {
			write(w, 200, state)
			return
		}
	}
	write(w, 200, coreruntime.State{ExitID: id, Phase: "stopped", Reason: "没有活动受管核心；保存配置不等于启动"})
}
func (a *Server) managedSave(w http.ResponseWriter, r *http.Request, s domain.Session) {
	a.stopped(func(w http.ResponseWriter, r *http.Request, _ domain.Session) {
		var body struct {
			Request        ingress.ManagedRequest `json:"request"`
			ExitPassword   string                 `json:"exit_password"`
			Confirm        bool                   `json:"confirm"`
			ConfirmRead    bool                   `json:"confirm_read"`
			ConfirmNetwork bool                   `json:"confirm_network"`
		}
		if !decode(w, r, &body) {
			return
		}
		if !body.Confirm {
			fail(w, 400, "confirm_required", "明确确认新建或替换独立受管入口，不接管已有面板")
			return
		}
		plan, e := ingress.ManagedPlanFor(body.Request)
		if e != nil {
			fail(w, 400, "ingress_invalid", e.Error())
			return
		}
		id := r.PathValue("id")
		exit, hash, e := a.store.ExitForRuntime(id)
		if e != nil {
			a.runtimeError(w, e)
			return
		}
		if len(body.ExitPassword) > 255 || !security.VerifyPassword(hash, body.ExitPassword) {
			fail(w, 403, "exit_credentials_invalid", "逻辑出口入口密码不匹配；不会擅自重置凭据")
			return
		}
		if !a.assetGate(w) {
			return
		}
		defer func() { <-a.assetSlot }()
		var asset *certassets.Asset
		var target *ingress.RealityTarget
		if plan.NeedsCertificate {
			if !body.ConfirmRead {
				fail(w, 400, "confirm_read_required", "TLS模板需同意自动查找证书")
				return
			}
			_, asset, e = a.discoverCertificate(plan.Request.ServerName)
			if e != nil || asset == nil {
				fail(w, 422, "certificate_unavailable", "当前账号无法取得已验证域名证书，请通过授权安装助手准备，私钥不会回显")
				return
			}
		}
		if plan.NeedsRealityTarget {
			if !body.ConfirmNetwork {
				fail(w, 400, "confirm_network_required", "REALITY生成需同意重新检测目标，不能用浏览器结果替代")
				return
			}
			checked, e := a.probeReality(r.Context(), plan.Request.RealityHostname)
			if e != nil {
				fail(w, 422, "target_unverified", ingress.ErrReality.Error())
				return
			}
			target = &checked
		}
		bundle, e := ingress.GenerateManaged(body.Request, ingress.Proxy{Port: exit.Port, Username: exit.Username, Password: body.ExitPassword}, asset, ingress.ManagedFiles{Certificate: "/miku-inline/certificate.pem", PrivateKey: "/miku-inline/private.key"}, target)
		if e != nil {
			a.coreError(w, e)
			return
		}
		if bundle.Material == nil {
			a.coreError(w, coreruntime.ErrCore)
			return
		}
		if e = a.store.SaveManaged(id, *bundle.Material, hash); e != nil {
			a.coreError(w, e)
			return
		}
		view, e := a.store.ManagedView(id)
		if e != nil {
			a.runtimeError(w, e)
			return
		}
		write(w, 200, view)
	})(w, r, s)
}
func (a *Server) managedRemove(w http.ResponseWriter, r *http.Request, s domain.Session) {
	a.stopped(func(w http.ResponseWriter, r *http.Request, _ domain.Session) {
		if e := a.store.DeleteManaged(r.PathValue("id")); e != nil {
			a.runtimeError(w, e)
			return
		}
		w.WriteHeader(204)
	})(w, r, s)
}
func (a *Server) managedClient(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var consent struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &consent) {
		return
	}
	if !consent.Confirm {
		fail(w, 400, "confirm_required", "客户端导入含入口使用凭据，请明确确认显示")
		return
	}
	m, _, ok, e := a.store.Managed(r.PathValue("id"))
	if e != nil {
		a.runtimeError(w, e)
		return
	}
	if !ok {
		fail(w, 404, "ingress_not_found", "尚未保存入口")
		return
	}
	bundle, e := m.Render()
	if e != nil {
		a.coreError(w, e)
		return
	}
	a.store.AddEvent("info", "ingress.client_revealed", "管理员明确显示入口客户端材料，未返回服务端秘密", r.PathValue("id"))
	write(w, 200, map[string]any{"client_config": string(bundle.ClientJSON), "uri": bundle.URI, "server_private_key_returned": false, "proxy_password_returned": false})
}
func (a *Server) managedStart(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var consent struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &consent) {
		return
	}
	if !consent.Confirm {
		fail(w, 400, "confirm_required", "明确确认启动加密入口监听，不开放防火墙或修改已有面板")
		return
	}
	if a.cores == nil {
		a.coreError(w, coreruntime.ErrUnavailable)
		return
	}
	state, e := a.cores.Start(r.Context(), r.PathValue("id"))
	if e != nil {
		a.coreError(w, e)
		return
	}
	write(w, 200, state)
}
func (a *Server) managedStop(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !a.requireExit(w, r.PathValue("id")) {
		return
	}
	if a.cores != nil {
		if e := a.cores.Stop(r.Context(), r.PathValue("id")); e != nil {
			a.coreError(w, e)
			return
		}
	}
	w.WriteHeader(204)
}
