package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"mikugate/internal/certassets"
	"mikugate/internal/coreruntime"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
)

func (a *Server) entranceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/entrance-panels/inspect", a.auth(a.entranceAction(a.entryPanelInspect)))
	mux.HandleFunc("POST /api/entrance-panels/attach", a.auth(a.entranceAction(a.entryPanelAttach)))
	mux.HandleFunc("GET /api/entrances", a.auth(a.entrancesList))
	mux.HandleFunc("POST /api/entrances", a.auth(a.entranceAction(a.entranceCreate)))
	mux.HandleFunc("GET /api/entrances/{entry}", a.auth(a.entranceGet))
	mux.HandleFunc("PUT /api/entrances/{entry}", a.auth(a.entranceAction(a.entranceStopped(a.entranceUpdate))))
	mux.HandleFunc("DELETE /api/entrances/{entry}", a.auth(a.entranceAction(a.entranceStopped(a.entranceDelete))))
	mux.HandleFunc("POST /api/entrances/{entry}/landings", a.auth(a.entranceAction(a.entranceStopped(a.entranceBind))))
	mux.HandleFunc("DELETE /api/entrances/{entry}/landings/{exit}", a.auth(a.entranceAction(a.entranceStopped(a.entranceUnbind))))
	mux.HandleFunc("POST /api/entrances/{entry}/landings/{exit}/client", a.auth(a.entranceClient))
	mux.HandleFunc("POST /api/entrances/{entry}/start", a.auth(a.entranceAction(a.entranceStart)))
	mux.HandleFunc("POST /api/entrances/{entry}/stop", a.auth(a.entranceAction(a.entranceStop)))
}

func (a *Server) entranceAction(fn func(http.ResponseWriter, *http.Request, domain.Session)) func(http.ResponseWriter, *http.Request, domain.Session) {
	return func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		select {
		case a.entranceOperations <- struct{}{}:
			defer func() { <-a.entranceOperations }()
		default:
			fail(w, 409, "entrance_busy", "入口操作正在进行，请稍后重试")
			return
		}
		fn(w, r, s)
	}
}

func (a *Server) entranceStopped(fn func(http.ResponseWriter, *http.Request, domain.Session)) func(http.ResponseWriter, *http.Request, domain.Session) {
	return func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		_, material, err := a.store.Entrance(r.PathValue("entry"))
		if err != nil {
			a.storeError(w, err)
			return
		}
		if material.External != nil {
			if material.External.AppliedSHA != "" && !material.External.Paused {
				fail(w, 409, "entrance_running", "先停止此面板入口，再修改入口或落地")
				return
			}
			fn(w, r, s)
			return
		}
		operation := func() error { fn(w, r, s); return nil }
		if a.cores != nil {
			if err := a.cores.WithStopped(coreruntime.EntranceKey(r.PathValue("entry")), operation); err != nil {
				a.coreError(w, err)
			}
		} else {
			operation()
		}
	}
}

func (a *Server) entrancesList(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	views, err := a.store.Entrances()
	if err != nil {
		a.storeError(w, err)
		return
	}
	items := []map[string]any{}
	for _, view := range views {
		state := coreruntime.State{ExitID: coreruntime.EntranceKey(view.ID), Phase: "stopped", Reason: "入口已保存；落地独立启动后可开启入口"}
		_, material, e := a.store.Entrance(view.ID)
		if e != nil {
			a.storeError(w, e)
			return
		}
		if material.External != nil && material.External.AppliedSHA != "" && !material.External.Paused {
			state.Phase = "external-configured"
			state.Reason = "已通过已有面板保存落地路由"
		}
		if a.cores != nil {
			if live, ok := a.cores.Snapshot(coreruntime.EntranceKey(view.ID)); ok {
				state = live
			}
		}
		items = append(items, map[string]any{"entrance": view, "runtime": state})
	}
	write(w, 200, items)
}

func (a *Server) entranceGet(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	view, _, err := a.store.Entrance(r.PathValue("entry"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 200, view)
}

func (a *Server) buildEntrance(ctx context.Context, request ingress.ManagedRequest, identity *ingress.ManagedIdentity) (ingress.ManagedMaterial, error) {
	if request.Port == 0 {
		port, err := a.store.FreeEntrancePort()
		if err != nil {
			return ingress.ManagedMaterial{}, err
		}
		request.Port = port
	}
	plan, err := ingress.ManagedPlanFor(request)
	if err != nil {
		return ingress.ManagedMaterial{}, err
	}
	var asset *certassets.Asset
	var target *ingress.RealityTarget
	if plan.NeedsCertificate {
		_, asset, err = a.discoverCertificate(plan.Request.ServerName)
		if err != nil || asset == nil {
			return ingress.ManagedMaterial{}, errors.New("此域名尚无可用证书；可先用REALITY，之后用 miku domain 绑定域名")
		}
	}
	if plan.NeedsRealityTarget {
		checked, e := a.probeReality(ctx, plan.Request.RealityHostname)
		if e != nil {
			return ingress.ManagedMaterial{}, e
		}
		target = &checked
	}
	proxyPassword, err := security.Token(18)
	if err != nil {
		return ingress.ManagedMaterial{}, err
	}
	proxyPort := 20000
	if proxyPort == request.Port {
		proxyPort++
	}
	bundle, err := ingress.GenerateManaged(request, ingress.Proxy{Port: proxyPort, Username: "miku-unbound", Password: proxyPassword}, asset, ingress.ManagedFiles{Certificate: "/miku-inline/certificate.pem", PrivateKey: "/miku-inline/private.key"}, target)
	if err != nil || bundle.Material == nil {
		return ingress.ManagedMaterial{}, err
	}
	material := *bundle.Material
	if identity != nil {
		material.Identity.UUID, material.Identity.Password = identity.UUID, identity.Password
		if plan.NeedsRealityTarget && identity.RealityPrivateKey != "" {
			material.Identity.RealityPrivateKey, material.Identity.ShortID = identity.RealityPrivateKey, identity.ShortID
		}
	}
	return material, nil
}

func (a *Server) entranceCreate(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Name    string                 `json:"name"`
		Request ingress.ManagedRequest `json:"request"`
		Confirm bool                   `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认新建入口节点")
		return
	}
	if !a.assetGate(w) {
		return
	}
	defer func() { <-a.assetSlot }()
	material, err := a.buildEntrance(r.Context(), input.Request, nil)
	if err != nil {
		fail(w, 400, "entrance_invalid", err.Error())
		return
	}
	if a.cores == nil {
		a.coreError(w, coreruntime.ErrUnavailable)
		return
	}
	bundle, err := material.Render()
	if err == nil {
		err = a.cores.ValidateConfig(r.Context(), material.Request.Core, bundle.ServerJSON)
	}
	if err != nil {
		a.coreError(w, err)
		return
	}
	view, err := a.store.CreateEntrance(input.Name, "miku", material)
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 201, view)
}

func (a *Server) entranceUpdate(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Name     string                 `json:"name"`
		Request  ingress.ManagedRequest `json:"request"`
		Revision string                 `json:"revision"`
		Confirm  bool                   `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认修改入口核心或域名")
		return
	}
	view, old, err := a.store.Entrance(r.PathValue("entry"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if old.External != nil {
		input.Request.Port = 0
	} else if input.Request.Port == 0 {
		input.Request.Port = view.Request.Port
	}
	if input.Revision != view.Revision {
		fail(w, 409, "entrance_changed", "入口已变化，请刷新")
		return
	}
	if !a.assetGate(w) {
		return
	}
	defer func() { <-a.assetSlot }()
	material, err := a.buildEntrance(r.Context(), input.Request, &old.Identity)
	if err != nil {
		fail(w, 400, "entrance_invalid", err.Error())
		return
	}
	_, _, routes, err := a.store.EntranceRoutes(view.ID)
	if err != nil {
		a.storeError(w, err)
		return
	}
	bundle, err := material.Render()
	raw := bundle.ServerJSON
	if err == nil && len(routes) > 0 {
		raw, err = ingress.RenderLandings(material, routes)
	}
	if err == nil && a.cores != nil {
		err = a.cores.ValidateConfig(r.Context(), material.Request.Core, raw)
	}
	if err != nil {
		a.coreError(w, err)
		return
	}
	if err = a.store.UpdateEntrance(view.ID, input.Name, view.Revision, material); err != nil {
		a.storeError(w, err)
		return
	}
	view, _, err = a.store.Entrance(view.ID)
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 200, view)
}

func (a *Server) entranceDelete(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.DeleteEntrance(r.PathValue("entry")); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) entranceBind(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		ExitID  string `json:"exit_id"`
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认绑定落地")
		return
	}
	if err := a.store.BindEntranceExit(r.PathValue("entry"), input.ExitID); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) entranceUnbind(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.UnbindEntranceExit(r.PathValue("entry"), r.PathValue("exit")); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *Server) entranceClient(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认显示节点链接")
		return
	}
	view, base, routes, err := a.store.EntranceRoutes(r.PathValue("entry"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	for _, route := range routes {
		if route.ExitID == r.PathValue("exit") {
			bundle, err := ingress.LandingClient(base, route)
			if err != nil {
				a.coreError(w, err)
				return
			}
			write(w, 200, map[string]any{"uri": bundle.URI, "client_config": string(bundle.ClientJSON), "entrance": view.ID, "landing": route.ExitID, "port": view.Request.Port})
			return
		}
	}
	fail(w, 404, "landing_missing", "此入口没有绑定所选落地")
}

func (a *Server) entranceStart(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	view, base, routes, err := a.store.EntranceRoutes(r.PathValue("entry"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if base.External != nil {
		if err = a.externalEntranceApply(r.Context(), view.ID, base, routes, false); err != nil {
			fail(w, 422, "panel_reuse_failed", err.Error())
			return
		}
		write(w, 200, map[string]any{"phase": "external-configured", "core": base.Request.Core, "reason": "已通过面板持久API复用原入口，并回读多落地路由；客户端路径请实测"})
		return
	}
	if a.cores == nil {
		a.coreError(w, coreruntime.ErrUnavailable)
		return
	}
	if base.Request.Template == "vless-reality-vision" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		checked, e := a.probeReality(ctx, base.Request.RealityHostname)
		cancel()
		if e != nil {
			a.coreError(w, e)
			return
		}
		base.Target = &checked
		if err = a.cores.WithStopped(coreruntime.EntranceKey(view.ID), func() error { return a.store.UpdateEntrance(view.ID, view.Name, view.Revision, base) }); err != nil {
			a.coreError(w, err)
			return
		}
	}
	state, err := a.cores.StartEntrance(r.Context(), view.ID, base, routes)
	if err != nil {
		a.coreError(w, err)
		return
	}
	write(w, 200, state)
}
func (a *Server) entranceStop(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	view, base, routes, err := a.store.EntranceRoutes(r.PathValue("entry"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if base.External != nil && len(routes) > 0 {
		if err = a.externalEntranceApply(r.Context(), view.ID, base, routes, true); err != nil {
			fail(w, 422, "panel_pause_failed", err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if a.cores != nil {
		if err := a.cores.Stop(r.Context(), coreruntime.EntranceKey(r.PathValue("entry"))); err != nil {
			a.coreError(w, err)
			return
		}
	}
	w.WriteHeader(204)
}
