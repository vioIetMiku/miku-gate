package httpapi

import (
	"context"
	"errors"
	"mikugate/internal/coreruntime"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"net/http"
	"time"
)

type coreSelection struct {
	Core     string `json:"core"`
	Rollback bool   `json:"rollback"`
}
type coreChangePlan struct {
	FromCore          string                 `json:"from_core"`
	ToCore            string                 `json:"to_core"`
	Revision          string                 `json:"revision"`
	Request           ingress.ManagedRequest `json:"request"`
	Rollback          bool                   `json:"rollback"`
	IdentityPreserved bool                   `json:"identity_preserved"`
	RequiresNetwork   bool                   `json:"requires_network_confirmation"`
	CoreValidated     bool                   `json:"core_validated"`
	RuntimeStarted    bool                   `json:"runtime_started"`
}

func selectCore(material ingress.ManagedMaterial, selection coreSelection) (string, error) {
	to := selection.Core
	if selection.Rollback {
		if to != "" || material.CoreRollback == nil || material.CoreRollback.Validate(material.Request.Core) != nil {
			return "", domain.ErrConflict
		}
		to = material.CoreRollback.PreviousCore
	}
	if (to != "xray" && to != "sing-box") || to == material.Request.Core {
		return "", ingress.ErrConfig
	}
	return to, nil
}
func (a *Server) managedCorePlan(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var selection coreSelection
	if !decode(w, r, &selection) {
		return
	}
	material, _, ok, revision, err := a.store.ManagedSnapshot(r.PathValue("id"))
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	if !ok {
		fail(w, 404, "ingress_not_found", "尚未保存入口")
		return
	}
	to, err := selectCore(material, selection)
	if err != nil {
		fail(w, 400, "core_selection_invalid", "请选择另一受管核心，或有效的上次核心回滚记录")
		return
	}
	request := material.Request
	request.Core = to
	if _, err = ingress.ManagedPlanFor(request); err != nil {
		a.coreError(w, err)
		return
	}
	write(w, 200, coreChangePlan{FromCore: material.Request.Core, ToCore: to, Revision: revision, Request: request, Rollback: selection.Rollback, IdentityPreserved: true, RequiresNetwork: request.Template == "vless-reality-vision"})
}
func (a *Server) managedCoreApply(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var input struct {
		Core           string `json:"core"`
		Rollback       bool   `json:"rollback"`
		Revision       string `json:"revision"`
		Confirm        bool   `json:"confirm"`
		ConfirmNetwork bool   `json:"confirm_network"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请先预览并明确确认停止后的核心切换；保留客户端身份，不自动启动")
		return
	}
	if a.cores == nil {
		a.coreError(w, coreruntime.ErrUnavailable)
		return
	}
	a.stopped(func(w http.ResponseWriter, r *http.Request, _ domain.Session) {
		id := r.PathValue("id")
		material, _, ok, revision, err := a.store.ManagedSnapshot(id)
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		if !ok {
			fail(w, 404, "ingress_not_found", "尚未保存入口")
			return
		}
		if input.Revision == "" || input.Revision != revision {
			fail(w, 409, "core_preview_changed", "已保存素材发生变化，请重新预览核心切换")
			return
		}
		to, err := selectCore(material, coreSelection{input.Core, input.Rollback})
		if err != nil {
			fail(w, 400, "core_selection_invalid", "核心或回滚记录无效")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		candidate := material
		candidate.Request.Core = to
		if input.Rollback {
			candidate.CoreRollback = nil
		} else {
			candidate.CoreRollback = &ingress.CoreRollback{PreviousCore: material.Request.Core, CurrentCore: to, ChangedAt: time.Now().UTC()}
		}
		if candidate.Request.Template == "vless-reality-vision" {
			if !input.ConfirmNetwork {
				fail(w, 400, "confirm_network_required", "REALITY切换前需明确同意重检已保存目标")
				return
			}
			if !a.assetGate(w) {
				return
			}
			defer func() { <-a.assetSlot }()
			target, err := a.probeReality(ctx, candidate.Request.RealityHostname)
			if err != nil {
				a.coreError(w, err)
				return
			}
			candidate.Target = &target
		}
		bundle, err := candidate.Render()
		if err != nil {
			a.coreError(w, err)
			return
		}
		if err = a.cores.ValidateConfig(ctx, to, bundle.ServerJSON); err != nil {
			a.coreError(w, err)
			return
		}
		_, hash, err := a.store.ExitForRuntime(id)
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		if err = a.store.ReplaceManaged(ctx, id, candidate, hash, revision); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				fail(w, 409, "core_preview_changed", "配置已变化，请重新预览；没有应用核心切换")
			} else {
				a.coreError(w, err)
			}
			return
		}
		view, err := a.store.ManagedView(id)
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		write(w, 200, map[string]any{"ingress": view, "core_validated": true, "runtime_started": false, "resume_ingress": false, "rollback_applied": input.Rollback})
	})(w, r, s)
}
