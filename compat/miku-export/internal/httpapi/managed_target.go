package httpapi

import (
	"context"
	"net/http"
	"time"

	"mikugate/internal/coreruntime"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
)

// Revalidate only the already saved host. No arbitrary host/address, template
// change, credential rotation or automatic restart is accepted here.
func (a *Server) refreshManagedTarget(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	refresh := func() error {
		material, _, has, revision, err := a.store.ManagedSnapshot(id)
		if err != nil {
			return err
		}
		if !has {
			return coreruntime.ErrUnavailable
		}
		plan, err := ingress.ManagedPlanFor(material.Request)
		if err != nil {
			return err
		}
		if !plan.NeedsRealityTarget {
			return ingress.ErrReality
		}
		target, err := a.probeReality(ctx, plan.Request.RealityHostname)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		material.Target = &target
		_, hash, err := a.store.ExitForRuntime(id)
		if err != nil {
			return err
		}
		return a.store.ReplaceManagedTarget(ctx, id, material, hash, revision)
	}
	if a.cores != nil {
		return a.cores.WithStopped(id, refresh)
	}
	return refresh()
}
func (a *Server) managedRefreshTarget(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var consent struct {
		Confirm        bool `json:"confirm"`
		ConfirmNetwork bool `json:"confirm_network"`
	}
	if !decode(w, r, &consent) {
		return
	}
	if !consent.Confirm || !consent.ConfirmNetwork {
		fail(w, 400, "confirm_network_required", "需要明确同意从VPS重新验证已保存REALITY目标；只更新该目标，保留客户端身份")
		return
	}
	if !a.requireExit(w, r.PathValue("id")) {
		return
	}
	refresh := func() error {
		if !a.assetGate(w) {
			return nil
		}
		defer func() { <-a.assetSlot }()
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		if err := a.refreshManagedTarget(ctx, r.PathValue("id")); err != nil {
			a.coreError(w, err)
			return nil
		}
		view, err := a.store.ManagedView(r.PathValue("id"))
		if err != nil {
			a.runtimeError(w, err)
			return nil
		}
		a.store.AddEvent("info", "ingress.target_revalidated", "仅重新验证已保存REALITY目标；客户端身份保留，未自动启动核心", r.PathValue("id"))
		write(w, 200, view)
		return nil
	}
	if a.recovery != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if err := a.recovery.Control(ctx, r.PathValue("id"), refresh); err != nil {
			fail(w, 409, "recovery_busy", "自动恢复/人工操作正在进行，稍后重试")
		}
	} else {
		refresh()
	}
}
