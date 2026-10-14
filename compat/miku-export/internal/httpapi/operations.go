package httpapi

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"mikugate/internal/coreruntime"
	"mikugate/internal/diagnostics"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/security"
	"mikugate/internal/tasks"
)

func (a *Server) initOperations(ctx context.Context) error {
	a.operationsSlot = make(chan struct{}, 1)
	a.remoteSlots = make(chan struct{}, 2)
	life := ctx
	if life == nil {
		life = context.Background()
	}
	manager, err := tasks.New(life, a.store, a.operateExit)
	if err != nil {
		return err
	}
	a.tasks = manager
	if ctx != nil && a.gateway != nil {
		go a.sampleHistory(ctx)
	}
	return nil
}
func (a *Server) operationGate(w http.ResponseWriter) bool {
	select {
	case a.operationsSlot <- struct{}{}:
		return true
	default:
		fail(w, 429, "operation_busy", "已有运维操作进行中")
		return false
	}
}
func (a *Server) operationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/nodes/preferences", a.auth(a.nodePreferences))
	mux.HandleFunc("PUT /api/nodes/{id}/preference", a.auth(a.nodePreferenceSave))
	mux.HandleFunc("DELETE /api/nodes/{id}/preference", a.auth(a.nodePreferenceRemove))
	mux.HandleFunc("GET /api/history", a.auth(a.history))
	mux.HandleFunc("GET /api/tasks", a.auth(a.taskList))
	mux.HandleFunc("POST /api/tasks", a.auth(a.taskSubmit))
	mux.HandleFunc("POST /api/tasks/{id}/cancel", a.auth(a.taskCancel))
	mux.HandleFunc("POST /api/auth/password", a.auth(a.passwordChange))
	mux.HandleFunc("POST /api/backup/export", a.auth(a.portableExport))
	mux.HandleFunc("GET /api/support/report", a.auth(a.supportReport))
	mux.HandleFunc("GET /api/certificates", a.auth(a.certificates))
	mux.HandleFunc("POST /api/exits/{id}/ingress/refresh-certificate", a.auth(a.certificateRefresh))
	mux.HandleFunc("GET /api/exits/{id}/standby", a.auth(a.standbyGet))
	mux.HandleFunc("PUT /api/exits/{id}/standby", a.auth(a.stopped(a.standbySave)))
	mux.HandleFunc("GET /api/exits/{id}/vpn/transport", a.auth(a.vpnTransportGet))
	mux.HandleFunc("PUT /api/exits/{id}/vpn/transport", a.auth(a.stopped(a.vpnTransportSave)))
	mux.HandleFunc("DELETE /api/exits/{id}/vpn/transport", a.auth(a.stopped(a.vpnTransportRemove)))
	mux.HandleFunc("GET /api/accounts", a.auth(a.accountList))
	mux.HandleFunc("POST /api/accounts", a.auth(a.accountCreate))
	mux.HandleFunc("PUT /api/accounts/{username}", a.auth(a.accountUpdate))
	mux.HandleFunc("DELETE /api/accounts/{username}", a.auth(a.accountDelete))
	a.fleetRoutes(mux)
}
func (a *Server) nodePreferences(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	v, err := a.store.NodePreferences()
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, v)
}
func (a *Server) nodePreferenceSave(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		Favorite bool     `json:"favorite"`
		Blocked  bool     `json:"blocked"`
		Tags     []string `json:"tags"`
		Note     string   `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := domain.NodePreference{NodeID: r.PathValue("id"), Favorite: in.Favorite, Blocked: in.Blocked, Tags: in.Tags, Note: in.Note}
	if err := a.store.SaveNodePreference(p); err != nil {
		fail(w, 400, "preference_invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}
func (a *Server) nodePreferenceRemove(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.DeleteNodePreference(r.PathValue("id")); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) history(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if id := r.URL.Query().Get("exit_id"); id != "" && !sessionAllows(s, id) {
		fail(w, 403, "permission_denied", "出口不在账号范围")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := a.store.History(r.URL.Query().Get("exit_id"), limit)
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, map[string]any{"samples": func() []domain.HistorySample {
		out := []domain.HistorySample{}
		for _, p := range v {
			if sessionAllows(s, p.ExitID) {
				out = append(out, p)
			}
		}
		return out
	}(), "interval_seconds": 60, "max_records": 20000, "retention_days": 14, "scope": "periodic proxy runtime snapshots; counters reset per run; not billing"})
}
func (a *Server) eventsQuery(w http.ResponseWriter, r *http.Request, s domain.Session) {
	q := r.URL.Query()
	if id := q.Get("exit_id"); id != "" && !sessionAllows(s, id) {
		fail(w, 403, "permission_denied", "出口不在账号范围")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	v, err := a.store.QueryEvents(q.Get("level"), q.Get("exit_id"), before, limit)
	if err != nil {
		fail(w, 400, "event_query_invalid", err.Error())
		return
	}
	out := []domain.Event{}
	for _, e := range v {
		if s.Role == "admin" || sessionAllows(s, e.ExitID) {
			out = append(out, e)
		}
	}
	write(w, 200, out)
}
func (a *Server) sampleHistory(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	previous := map[string]string{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			exits, err := a.store.ListExits()
			if err != nil {
				continue
			}
			samples := []domain.HistorySample{}
			for _, exit := range exits {
				p := domain.HistorySample{ExitID: exit.ID, Phase: exit.Status}
				state, active := a.gateway.Snapshot(exit.ID)
				if active {
					p.Phase = state.Phase
					p.Healthy = state.Healthy
					p.ExitIP = state.Proof.ExitIP
					p.CheckMS = state.CheckMilliseconds
				}
				if t, ok := a.gateway.Traffic(exit.ID); ok {
					p.Connections = int64(t.ActiveConnections)
					p.Upload = int64(t.UploadBytes)
					p.Download = int64(t.DownloadBytes)
					p.RunSince = t.Since.Format(time.RFC3339Nano)
				}
				encoded, _ := json.Marshal(p)
				if active || previous[exit.ID] != string(encoded) {
					samples = append(samples, p)
					previous[exit.ID] = string(encoded)
				}
			}
			if len(samples) > 0 && a.store.RecordHistory(samples) != nil {
				_ = a.store.AddEvent("warning", "history.write_failed", "历史采样未保存，请检查数据目录与空间", "")
			}
		}
	}
}
func (a *Server) taskList(w http.ResponseWriter, r *http.Request, s domain.Session) {
	jobs := []tasks.Job{}
	for _, j := range a.tasks.List() {
		allowed := true
		for _, item := range j.Items {
			if !sessionAllows(s, item.ExitID) {
				allowed = false
			}
		}
		if allowed {
			jobs = append(jobs, j)
		}
	}
	write(w, 200, jobs)
}
func (a *Server) taskSubmit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in struct {
		Action  string   `json:"action"`
		ExitIDs []string `json:"exit_ids"`
		Confirm bool     `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "请确认批量操作及可能的连接中断")
		return
	}
	for _, id := range in.ExitIDs {
		if !sessionAllows(s, id) {
			fail(w, 403, "permission_denied", "任务出口超出账号授权范围")
			return
		}
		if !a.requireExit(w, id) {
			return
		}
	}
	job, err := a.tasks.Submit(in.Action, in.ExitIDs)
	if err != nil {
		fail(w, 409, "task_rejected", err.Error())
		return
	}
	write(w, 202, job)
}
func (a *Server) taskCancel(w http.ResponseWriter, r *http.Request, s domain.Session) {
	for _, job := range a.tasks.List() {
		if job.ID == r.PathValue("id") {
			for _, item := range job.Items {
				if !sessionAllows(s, item.ExitID) {
					fail(w, 403, "permission_denied", "任务包含未授权出口")
					return
				}
			}
		}
	}
	if err := a.tasks.Cancel(r.PathValue("id")); err != nil {
		fail(w, 404, "task_not_found", err.Error())
		return
	}
	w.WriteHeader(204)
}
func (a *Server) operateExit(ctx context.Context, action, id string) error {
	if a.gateway == nil {
		return gateway.ErrUnavailable
	}
	operation := func() error {
		switch action {
		case "start":
			_, err := a.gateway.Start(ctx, id)
			return err
		case "stop":
			if a.cores != nil {
				if err := a.cores.Stop(ctx, id); err != nil {
					return err
				}
			}
			return a.gateway.Stop(ctx, id)
		case "probe":
			_, err := a.gateway.Check(ctx, id)
			return err
		case "start-ingress":
			if a.cores == nil {
				return coreruntime.ErrUnavailable
			}
			if err := a.refreshManagedTargetIfRequired(ctx, id); err != nil {
				return err
			}
			_, err := a.cores.Start(ctx, id)
			return err
		}
		return errors.New("任务操作无效")
	}
	if action == "stop" {
		if err := a.store.PauseRecovery(id); err != nil {
			return err
		}
		if a.recovery != nil {
			if err := a.recovery.Pause(id); err != nil {
				return err
			}
		}
	}
	var err error
	if a.recovery != nil {
		err = a.recovery.Control(ctx, id, operation)
	} else {
		err = operation()
	}
	if err != nil {
		return errors.New(gateway.Reason(err))
	}
	return nil
}
func (a *Server) refreshManagedTargetIfRequired(ctx context.Context, id string) error {
	m, _, ok, err := a.store.Managed(id)
	if err != nil {
		return err
	}
	if !ok {
		return gateway.ErrUnavailable
	}
	if m.Request.Template == "vless-reality-vision" {
		return a.refreshManagedTarget(ctx, id)
	}
	return nil
}
func (a *Server) passwordChange(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !a.operationGate(w) {
		return
	}
	defer func() { <-a.operationsSlot }()
	if err := a.store.ChangePassword(s.Username, in.Current, in.New); err != nil {
		fail(w, 400, "password_rejected", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "miku_session", Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(204)
}
func (a *Server) portableExport(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in struct {
		Passphrase string `json:"passphrase"`
		Password   string `json:"current_password"`
		Confirm    bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "备份含完整入口凭据，请确认并保管口令")
		return
	}
	if !a.operationGate(w) {
		return
	}
	defer func() { <-a.operationsSlot }()
	hash, err := a.store.PasswordHash(s.Username)
	if err != nil {
		a.internal(w, err)
		return
	}
	if !security.VerifyPassword(hash, in.Password) {
		fail(w, 403, "password_required", "管理员密码校验失败")
		return
	}
	raw, err := a.store.ExportPortable(r.Context(), in.Passphrase)
	if err != nil {
		a.internal(w, err)
		return
	}
	defer clear(raw)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="miku-gate-`+time.Now().UTC().Format("20060102-150405")+`.mikubak"`)
	_ = a.store.AddEvent("info", "backup.portable_exported", "已导出完整口令加密备份，未停止数据面", "")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (a *Server) supportReport(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	instance, err := a.store.InstanceID()
	if err != nil {
		a.internal(w, err)
		return
	}
	events, err := a.store.Events()
	if err != nil {
		a.internal(w, err)
		return
	}
	safeEvents := []map[string]any{}
	for _, e := range events {
		safeEvents = append(safeEvents, map[string]any{"kind": e.Kind, "level": e.Level, "created_at": e.CreatedAt})
	}
	states := []gateway.State{}
	if a.gateway != nil {
		states = a.gateway.States()
	}
	write(w, 200, map[string]any{"format": 1, "version": domain.Version, "generated_at": time.Now().UTC(), "instance": instance, "diagnostics": diagnostics.Inspect(), "runtimes": states, "events": safeEvents, "secrets_included": false, "business_targets_included": false})
}
func (a *Server) certificates(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return
	}
	out := []map[string]any{}
	for _, exit := range exits {
		m, _, ok, revision, err := a.store.ManagedSnapshot(exit.ID)
		if err != nil {
			a.internal(w, err)
			return
		}
		if !ok || len(m.CertificatePEM) == 0 {
			continue
		}
		block, _ := pem.Decode(m.CertificatePEM)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"exit_id": exit.ID, "name": exit.Name, "hostname": m.Request.ServerName, "not_before": cert.NotBefore, "not_after": cert.NotAfter, "days_remaining": int(math.Floor(time.Until(cert.NotAfter).Hours() / 24)), "revision": revision})
	}
	write(w, 200, map[string]any{"ingresses": out, "management": a.managementIdentity, "private_material_included": false})
}
func (a *Server) certificateRefresh(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in struct {
		Revision string `json:"revision"`
		Confirm  bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "需确认停止后的证书更新")
		return
	}
	a.stopped(func(w http.ResponseWriter, r *http.Request, _ domain.Session) {
		id := r.PathValue("id")
		m, _, ok, revision, err := a.store.ManagedSnapshot(id)
		if err != nil || !ok {
			fail(w, 404, "ingress_not_found", "入口不存在")
			return
		}
		if revision != in.Revision {
			fail(w, 409, "preview_changed", "入口变化，请重新读取证书状态")
			return
		}
		if m.Request.Template == "vless-reality-vision" {
			fail(w, 400, "certificate_not_required", "REALITY无需域名证书")
			return
		}
		_, asset, err := a.discoverCertificate(m.Request.ServerName)
		if err != nil || asset == nil {
			fail(w, 422, "certificate_unavailable", "未找到有效的匹配证书，请先通过维护工具签发/续期")
			return
		}
		cert, key := asset.PEM()
		defer clear(key)
		m.CertificatePEM = cert
		m.PrivateKeyPEM = key
		bundle, err := m.Render()
		if err != nil {
			a.coreError(w, err)
			return
		}
		if a.cores == nil {
			a.coreError(w, coreruntime.ErrUnavailable)
			return
		}
		if err = a.cores.ValidateConfig(r.Context(), m.Request.Core, bundle.ServerJSON); err != nil {
			a.coreError(w, err)
			return
		}
		_, hash, err := a.store.ExitForRuntime(id)
		if err == nil {
			err = a.store.ReplaceManaged(r.Context(), id, m, hash, revision)
		}
		if err != nil {
			a.coreError(w, err)
			return
		}
		write(w, 200, map[string]any{"updated": true, "identities_preserved": true, "runtime_started": false})
	})(w, r, s)
}

func (a *Server) standbyGet(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	p, err := a.store.StandbyPolicy(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	var state *gateway.StandbyStatus
	if a.gateway != nil {
		if runtime, ok := a.gateway.Snapshot(r.PathValue("id")); ok {
			state = runtime.Standby
		}
	}
	write(w, 200, map[string]any{"policy": p, "runtime": state, "executor_session_budget": 3, "additional_sessions": 1, "replenish_interval_seconds": 120, "same_country_only": true})
}
func (a *Server) standbySave(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var p domain.StandbyPolicy
	if !decode(w, r, &p) {
		return
	}
	if p.Enabled && a.vpnClient == nil {
		fail(w, 409, "vpn_executor_required", "真实热备需要已配置独立VPN执行器")
		return
	}
	if err := a.store.SaveStandby(r.PathValue("id"), p); err != nil {
		fail(w, 400, "standby_invalid", err.Error())
		return
	}
	w.WriteHeader(204)
}
