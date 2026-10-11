package httpapi

import (
	"context"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/ingress"
	"mikugate/internal/recovery"
	"mikugate/internal/vpngate"
	"net/http"
	"time"
)

func (a *Server) createRecovery(ctx context.Context) {
	if a.gateway == nil {
		return
	}
	a.recovery = recovery.New(ctx, a.store, a.gateway, recovery.Options{Prepare: func(ctx context.Context, exit domain.Exit, p domain.RecoveryPolicy, nodeID string) error {
		if nodeID == "" {
			_, _, hasUpstream, e := a.store.Upstream(exit.ID)
			if e != nil {
				return e
			}
			if hasUpstream {
				return nil
			}
			binding, configured, e := a.store.VPN(exit.ID)
			if e != nil {
				return e
			}
			if configured {
				node, e := a.store.NodeForRuntime(binding.NodeID)
				if e != nil || node.Country != exit.Country {
					return vpngate.ErrIdentity
				}
			}
			return nil
		}
		node, e := a.store.NodeForRuntime(nodeID)
		if e != nil || node.Country != exit.Country || node.Source != vpngate.Source || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
			return vpngate.ErrIdentity
		}
		identity, e := a.inspectIdentity(ctx, node)
		if e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return a.gateway.WithStopped(exit.ID, func() error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return a.store.SaveVerifiedVPN(exit.ID, domain.VPNInput{NodeID: nodeID, UseVerifiedIdentity: true, AllowLegacy: p.AllowLegacy, Confirm: true}, identity)
		})
	}, StopIngress: func(ctx context.Context, id string) error {
		if a.cores != nil {
			return a.cores.Stop(ctx, id)
		}
		return nil
	}, ResumeIngress: func(ctx context.Context, id string) error {
		if a.cores == nil {
			return gateway.ErrUnavailable
		}
		material, _, has, e := a.store.Managed(id)
		if e != nil || !has {
			return gateway.ErrUnavailable
		}
		plan, e := ingress.ManagedPlanFor(material.Request)
		if e != nil {
			return e
		}
		if plan.NeedsRealityTarget {
			target, e := a.probeReality(ctx, material.Request.RealityHostname)
			if e != nil {
				return e
			}
			material.Target = &target
			_, hash, e := a.store.ExitForRuntime(id)
			if e != nil {
				return e
			}
			e = a.cores.WithStopped(id, func() error { return a.store.SaveManaged(id, material, hash) })
			if e != nil {
				return e
			}
		}
		_, e = a.cores.Start(ctx, id)
		return e
	}})
}
func (a *Server) getRecovery(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	p, e := a.store.Recovery(r.PathValue("id"))
	if e != nil {
		a.storeError(w, e)
		return
	}
	status := recovery.Status{Phase: "unavailable", Reason: "运行时未配置，自动恢复不启动"}
	if a.recovery != nil {
		status = a.recovery.Status(r.PathValue("id"))
		status.Enabled = p.Enabled
	}
	write(w, 200, map[string]any{"policy": p, "status": status, "country_constraint": "same configured exit country only", "max_candidates": 3, "business_direct_fallback": false})
}
func (a *Server) saveRecovery(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var p domain.RecoveryPolicy
	if !decode(w, r, &p) {
		return
	}
	if p.Validate() != nil {
		fail(w, 400, "recovery_invalid", "需明确授权，最多3个唯一候选；用户停止会取消自动恢复")
		return
	}
	if a.recovery == nil {
		fail(w, 409, "recovery_unavailable", "未配置实际运行时")
		return
	}
	id := r.PathValue("id")
	if !p.Enabled {
		if e := a.recovery.Pause(id); e != nil {
			a.storeError(w, e)
			return
		}
		write(w, 200, map[string]any{"enabled": false, "runtime_stopped": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	e := a.recovery.Control(ctx, id, func() error {
		if len(p.NodeIDs) > 0 && a.vpnClient == nil {
			return gateway.ErrVPNDisabled
		}
		return a.store.SaveRecovery(id, p)
	})
	if e != nil {
		fail(w, 409, "recovery_selection_rejected", "候选需官方新鲜同国家且不混用代理上游；没有住宅证据不允许严格住宅策略，或有操作进行中")
		return
	}
	a.recovery.Wake(id)
	a.store.AddEvent("info", "exit.recovery_enabled", "管理员明确授权故障/控制重启后的恢复；候选同国家、逐个身份验证、退避且不直连", id)
	write(w, 200, map[string]any{"enabled": true, "starts_after_seconds": 5, "credentials_rotated": false})
}

func (a *Server) startExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if a.recovery == nil {
		a.startExitLocked(w, r, s)
		return
	}
	e := a.recovery.Control(r.Context(), r.PathValue("id"), func() error { a.startExitLocked(w, r, s); return nil })
	if e != nil {
		fail(w, 409, "recovery_busy", "自动恢复/人工操作正在进行，稍后重试")
	}
}
func (a *Server) stopExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if a.recovery == nil {
		a.stopExitLocked(w, r, s)
		return
	}
	if e := a.recovery.Pause(r.PathValue("id")); e != nil {
		a.storeError(w, e)
		return
	}
	e := a.recovery.Control(r.Context(), r.PathValue("id"), func() error { a.stopExitLocked(w, r, s); return nil })
	if e != nil {
		fail(w, 409, "recovery_busy", "自动恢复已持久暂停，待取消完成后重试停止")
	}
}
