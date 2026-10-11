package httpapi

import (
	"context"
	"errors"
	"net/http"

	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/secretstore"
	"mikugate/internal/vpnipc"
)

func (a *Server) mode() string {
	if a.gateway != nil && a.vpnClient != nil {
		return "isolated-gateway-runtime"
	}
	if a.gateway != nil {
		return "proxy-upstream-runtime"
	}
	return "control-plane-only"
}
func (a *Server) stopped(fn func(http.ResponseWriter, *http.Request, domain.Session)) func(http.ResponseWriter, *http.Request, domain.Session) {
	return func(w http.ResponseWriter, r *http.Request, session domain.Session) {
		operation := func() error {
			if a.gateway == nil {
				fn(w, r, session)
				return nil
			}
			return a.gateway.WithStopped(r.PathValue("id"), func() error { fn(w, r, session); return nil })
		}
		if a.cores != nil {
			if err := a.cores.WithStopped(r.PathValue("id"), operation); err != nil {
				a.coreError(w, err)
			}
			return
		}
		if err := operation(); err != nil {
			a.runtimeError(w, err)
		}
	}
}

func (a *Server) listExits(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return
	}
	for i := range exits {
		if exits[i].Status != "stopped" && exits[i].Status != "blocked" {
			exits[i].Status = "stopped"
			exits[i].Reason = "没有可核验的活动运行时"
		}
		if a.gateway != nil {
			if state, ok := a.gateway.Snapshot(exits[i].ID); ok {
				exits[i].Status = state.Phase
				exits[i].Reason = state.Reason
			}
		}
	}
	write(w, 200, exits)
}
func (a *Server) getUpstream(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	config, updated, exists, err := a.store.Upstream(r.PathValue("id"))
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	if !exists {
		write(w, 200, domain.UpstreamView{Configured: false})
		return
	}
	write(w, 200, config.View(updated))
}
func (a *Server) saveUpstream(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in domain.UpstreamInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		fail(w, 400, "invalid_upstream", err.Error())
		return
	}
	if err := a.store.SaveUpstream(r.PathValue("id"), in); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			fail(w, 409, "upstream_change_requires_confirmation", "更换凭据目标不能复用旧密码；替换VPN绑定还需明确确认")
			return
		}
		a.runtimeError(w, err)
		return
	}
	if err := a.store.SetStatus(r.PathValue("id"), "stopped", "代理上游配置已保存；尚未验证或启动"); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *Server) removeUpstream(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.DeleteUpstream(r.PathValue("id")); err != nil {
		a.runtimeError(w, err)
		return
	}
	if err := a.store.SetStatus(r.PathValue("id"), "stopped", "代理上游已移除；生产OpenVPN驱动仍未接通"); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *Server) runtimeState(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	e, _, err := a.store.ExitForRuntime(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if a.gateway != nil {
		if state, ok := a.gateway.Snapshot(e.ID); ok {
			write(w, 200, state)
			return
		}
	}
	phase := e.Status
	if phase != "stopped" && phase != "blocked" {
		phase = "stopped"
	}
	write(w, 200, gateway.State{ExitID: e.ID, Phase: phase, Reason: e.Reason})
}
func (a *Server) checkRuntime(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if a.gateway == nil {
		a.runtimeError(w, gateway.ErrUnavailable)
		return
	}
	state, err := a.gateway.Check(r.Context(), r.PathValue("id"))
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	write(w, 200, state)
}
func (a *Server) runtimeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		a.storeError(w, err)
	case errors.Is(err, domain.ErrConflict):
		a.storeError(w, err)
	case errors.Is(err, gateway.ErrBusy):
		fail(w, 409, "runtime_busy", gateway.Reason(err))
	case errors.Is(err, gateway.ErrCapacity):
		fail(w, 409, "runtime_capacity", gateway.Reason(err))
	case errors.Is(err, gateway.ErrVPNDisabled):
		fail(w, 409, "vpn_executor_disabled", gateway.Reason(err))
	case errors.Is(err, vpnipc.ErrProfile):
		fail(w, 400, "vpn_profile_rejected", gateway.Reason(err))
	case errors.Is(err, vpnipc.ErrBusy):
		fail(w, 409, "vpn_executor_busy", gateway.Reason(err))
	case errors.Is(err, vpnipc.ErrUnavailable):
		fail(w, 502, "vpn_executor_unavailable", gateway.Reason(err))
	case errors.Is(err, gateway.ErrUnavailable):
		fail(w, 409, "executor_unavailable", gateway.Reason(err))
	case errors.Is(err, gateway.ErrListen):
		fail(w, 409, "port_unavailable", gateway.Reason(err))
	case errors.Is(err, gateway.ErrPolicy):
		fail(w, 409, "policy_unverified", gateway.Reason(err))
	case errors.Is(err, gateway.ErrClosed):
		fail(w, 503, "runtime_unavailable", "代理运行时或该会话已停止，请刷新状态")
	case errors.Is(err, secretstore.ErrKey), errors.Is(err, secretstore.ErrCiphertext):
		fail(w, 503, "credential_store_unavailable", err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		fail(w, 504, "runtime_timeout", gateway.Reason(err))
	default:
		fail(w, 502, "upstream_unavailable", gateway.Reason(err))
	}
}
