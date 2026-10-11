package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/ingress"
	"mikugate/internal/paneladapter"
	"mikugate/internal/security"
	"net/http"
	"time"
)

type panelPreview struct {
	Client      paneladapter.Runtime
	Endpoint    paneladapter.Endpoint
	Snapshot    paneladapter.Snapshot
	Username    string
	SessionCSRF string
	Expires     time.Time
}
type panelBackup struct {
	Instance  string                `json:"instance"`
	Endpoint  paneladapter.Endpoint `json:"endpoint"`
	ExitID    string                `json:"exit_id"`
	Before    paneladapter.Snapshot `json:"before"`
	Candidate json.RawMessage       `json:"candidate"`
	Plan      ingress.Plan          `json:"plan"`
}

func (a *Server) panelSlot(w http.ResponseWriter) bool {
	select {
	case a.panelOperations <- struct{}{}:
		return true
	default:
		fail(w, 429, "panel_busy", "面板操作进行中，不允许并发配置/重复应用")
		return false
	}
}
func (a *Server) panelInspect(w http.ResponseWriter, r *http.Request, session domain.Session) {
	var in struct {
		Endpoint  paneladapter.Endpoint `json:"endpoint"`
		Username  string                `json:"username"`
		Password  string                `json:"password"`
		TwoFactor string                `json:"two_factor"`
		Confirm   bool                  `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm || in.Endpoint.Validate() != nil {
		fail(w, 400, "panel_consent", "需明确同意读取本机面板配置与发送登录凭据；只固定本机地址")
		return
	}
	if !a.panelSlot(w) {
		return
	}
	defer func() { <-a.panelOperations }()
	a.mu.Lock()
	limited := time.Since(a.lastPanel) < 3*time.Second
	if !limited {
		a.lastPanel = time.Now()
	}
	a.mu.Unlock()
	if limited {
		fail(w, 429, "panel_rate_limited", "面板登录检测至少间隔3秒")
		return
	}
	client, e := a.panelFactory(in.Endpoint)
	if e != nil {
		fail(w, 422, "panel_unavailable", paneladapter.ErrPanel.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	e = client.Login(ctx, in.Username, in.Password, in.TwoFactor)
	in.Password = ""
	in.TwoFactor = ""
	if e != nil {
		fail(w, 422, "panel_unavailable", paneladapter.ErrPanel.Error())
		return
	}
	snapshot, e := client.Read(ctx)
	if e != nil {
		fail(w, 422, "panel_unavailable", paneladapter.ErrPanel.Error())
		return
	}
	source, _ := ingress.FindSource("x-ui")
	report, e := ingress.Inspect(source, snapshot.Rendered)
	if e != nil {
		fail(w, 422, "panel_config_invalid", ingress.ErrConfig.Error())
		return
	}
	token, e := security.Token(18)
	if e != nil {
		a.internal(w, e)
		return
	}
	a.mu.Lock()
	for id, p := range a.panelPreviews {
		if time.Now().After(p.Expires) {
			delete(a.panelPreviews, id)
		}
	}
	if len(a.panelPreviews) >= 4 {
		a.mu.Unlock()
		fail(w, 429, "panel_preview_capacity", "最多4个短期预览，关闭/等待过期后再试")
		return
	}
	a.panelPreviews[token] = panelPreview{Client: client, Endpoint: in.Endpoint, Snapshot: snapshot, Username: session.Username, SessionCSRF: session.CSRF, Expires: time.Now().Add(5 * time.Minute)}
	a.mu.Unlock()
	time.AfterFunc(5*time.Minute, func() { a.mu.Lock(); delete(a.panelPreviews, token); a.mu.Unlock() })
	write(w, 200, map[string]any{"preview_id": token, "template_sha256": paneladapter.Hash(snapshot.Template), "rendered_sha256": paneladapter.Hash(snapshot.Rendered), "report": report, "expires_in_seconds": 300, "persistent_api": true, "applied": false, "login_password_saved": false})
}
func (a *Server) preview(id, username, csrf string) (panelPreview, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	value, ok := a.panelPreviews[id]
	return value, ok && value.Username == username && value.SessionCSRF == csrf && time.Now().Before(value.Expires)
}
func (a *Server) panelBackups(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	ids, e := a.store.PanelBackupIDs()
	if e != nil {
		a.internal(w, e)
		return
	}
	pending, e := a.store.PanelPending()
	if e != nil {
		a.internal(w, e)
		return
	}
	safe := []map[string]any{}
	for _, id := range ids {
		raw, e := a.store.PanelBackup(id)
		if e != nil {
			fail(w, 409, "panel_backup_unavailable", "面板快照与配套key无法认证")
			return
		}
		var backup panelBackup
		e = json.Unmarshal(raw, &backup)
		clear(raw)
		if e != nil {
			fail(w, 409, "panel_backup_unavailable", "面板快照格式未知")
			return
		}
		safe = append(safe, map[string]any{"backup_id": id, "endpoint": backup.Endpoint, "exit_id": backup.ExitID, "inbound_tag": backup.Plan.InboundTag, "contains_private_material": true, "recovery_required": id == pending})
	}
	write(w, 200, safe)
}
func (a *Server) panelPlan(w http.ResponseWriter, r *http.Request, session domain.Session) {
	var in struct {
		PreviewID    string `json:"preview_id"`
		InboundID    string `json:"inbound_id"`
		ExitID       string `json:"exit_id"`
		ExitPassword string `json:"exit_password"`
		TemplateSHA  string `json:"template_sha256"`
		RenderedSHA  string `json:"rendered_sha256"`
		Confirm      bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "panel_consent", "需明确确认生成所选入口的定向路由计划")
		return
	}
	preview, ok := a.preview(in.PreviewID, session.Username, session.CSRF)
	if !ok || paneladapter.Hash(preview.Snapshot.Template) != in.TemplateSHA || paneladapter.Hash(preview.Snapshot.Rendered) != in.RenderedSHA {
		fail(w, 409, "panel_preview_expired", "模板/预览已变化或过期，重新检测")
		return
	}
	exit, hash, e := a.store.ExitForRuntime(in.ExitID)
	if e != nil {
		a.storeError(w, e)
		return
	}
	if !security.VerifyPassword(hash, in.ExitPassword) {
		fail(w, 403, "exit_credentials_invalid", "逻辑出口密码不匹配")
		return
	}
	raw, plan, e := ingress.PreparePanelTemplate(preview.Snapshot.Rendered, preview.Snapshot.Template, in.RenderedSHA, in.TemplateSHA, in.InboundID, ingress.Proxy{Port: exit.Port, Username: exit.Username, Password: in.ExitPassword})
	clear(raw)
	in.ExitPassword = ""
	if e != nil {
		fail(w, 422, "panel_plan_invalid", "所选入口/持久模板不能生成受控路由，未写配置")
		return
	}
	write(w, 200, map[string]any{"plan": plan, "persistent_template": true, "private_material_returned": false, "applied": false})
}

func (a *Server) panelApply(w http.ResponseWriter, r *http.Request, s domain.Session) {
	a.panelChange(w, r, s, false)
}
func (a *Server) panelRollback(w http.ResponseWriter, r *http.Request, s domain.Session) {
	a.panelChange(w, r, s, true)
}
func (a *Server) panelChange(w http.ResponseWriter, r *http.Request, session domain.Session, rollback bool) {
	var in struct {
		CandidateSHA string `json:"candidate_sha256"`
		PreviewID    string `json:"preview_id"`
		InboundID    string `json:"inbound_id"`
		ExitID       string `json:"exit_id"`
		ExitPassword string `json:"exit_password"`
		TemplateSHA  string `json:"template_sha256"`
		RenderedSHA  string `json:"rendered_sha256"`
		BackupID     string `json:"backup_id"`
		Confirm      bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "panel_consent", "需明确确认持久路由变更/面板核心重启与备份")
		return
	}
	if !a.panelSlot(w) {
		return
	}
	defer func() { <-a.panelOperations }()
	preview, ok := a.preview(in.PreviewID, session.Username, session.CSRF)
	if !ok {
		fail(w, 409, "panel_preview_expired", "面板预览已过期，重新登录检测")
		return
	}
	if paneladapter.Hash(preview.Snapshot.Template) != in.TemplateSHA || paneladapter.Hash(preview.Snapshot.Rendered) != in.RenderedSHA {
		fail(w, 409, "panel_changed", paneladapter.ErrChanged.Error())
		return
	}
	instance, e := a.store.InstanceID()
	if e != nil {
		a.internal(w, e)
		return
	}
	target := panelBackup{Instance: instance, Endpoint: preview.Endpoint, ExitID: in.ExitID, Before: preview.Snapshot}
	candidate := paneladapter.Snapshot{TestURL: preview.Snapshot.TestURL}
	plan := ingress.Plan{}
	pending, e := a.store.PanelPending()
	if e != nil {
		a.internal(w, e)
		return
	}
	if pending != "" && (!rollback || in.BackupID != pending) {
		fail(w, 409, "panel_recovery_required", "有未完成面板事务，先恢复快照ID="+pending)
		return
	}
	if rollback {
		raw, e := a.store.PanelBackup(in.BackupID)
		if e != nil {
			fail(w, 409, "panel_backup_unavailable", "配套面板备份无法读取")
			return
		}
		var old panelBackup
		e = json.Unmarshal(raw, &old)
		clear(raw)
		if e != nil || old.Instance != instance || old.Endpoint != preview.Endpoint || (pending == "" && !bytes.Equal(old.Candidate, preview.Snapshot.Template)) {
			fail(w, 409, "panel_changed", "仅回滚同安装/面板且当前模板等于此前应用结果；外部编辑后拒绝覆盖")
			return
		}
		candidate = old.Before
		target.ExitID = old.ExitID
	} else {
		if a.gateway == nil {
			fail(w, 409, "exit_unavailable", "先启动并验证Miku逻辑出口")
			return
		}
		exit, hash, e := a.store.ExitForRuntime(in.ExitID)
		if e != nil {
			a.storeError(w, e)
			return
		}
		if !security.VerifyPassword(hash, in.ExitPassword) {
			fail(w, 403, "exit_credentials_invalid", "逻辑出口认证不匹配")
			return
		}
		proxy := ingress.Proxy{Port: exit.Port, Username: exit.Username, Password: in.ExitPassword}
		in.ExitPassword = ""
		generated, p, e := ingress.PreparePanelTemplate(preview.Snapshot.Rendered, preview.Snapshot.Template, in.RenderedSHA, in.TemplateSHA, in.InboundID, proxy)
		proxy.Password = ""
		if e != nil {
			fail(w, 422, "panel_plan_invalid", "持久模板候选无法生成，未修改面板")
			return
		}
		if in.CandidateSHA != p.CandidateSHA256 {
			clear(generated)
			fail(w, 409, "panel_changed", "所选路由与已预览计划不同，重新预览")
			return
		}
		candidate.Template, e = paneladapter.Canonical(generated)
		clear(generated)
		if e != nil {
			fail(w, 422, "panel_plan_invalid", ingress.ErrConfig.Error())
			return
		}
		plan = p
	}
	target.Candidate = candidate.Template
	target.Plan = plan
	exitLife := r.Context()
	operation := func() error {
		ctx, cancel := context.WithTimeout(exitLife, 20*time.Second)
		defer cancel()
		latest, e := preview.Client.Read(ctx)
		if e != nil {
			return e
		}
		if !latest.Same(preview.Snapshot) {
			return paneladapter.ErrChanged
		}
		raw, e := json.Marshal(target)
		if e != nil {
			return e
		}
		id, e := a.store.SavePanelBackup(raw)
		clear(raw)
		if e != nil {
			return e
		}
		pendingID := id
		if pending != "" {
			pendingID = pending
		} else {
			if e = a.store.SetPanelPending(id); e != nil {
				return e
			}
		}
		recoverOld := func() bool {
			recovery, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if preview.Client.Save(recovery, preview.Snapshot) != nil {
				return false
			}
			restored, e := preview.Client.Read(recovery)
			return e == nil && restored.Same(preview.Snapshot)
		}
		if e = preview.Client.Save(ctx, candidate); e == nil {
			var actual paneladapter.Snapshot
			actual, e = preview.Client.Read(ctx)
			if e == nil && !bytes.Equal(actual.Template, candidate.Template) {
				e = paneladapter.ErrPanel
			}
			if e == nil {
				if rollback {
					if !actual.Same(candidate) {
						e = paneladapter.ErrPanel
					}
				} else {
					e = paneladapter.Verify(preview.Snapshot, actual, plan)
				}
			}
		}
		if e != nil {
			if recoverOld() {
				if pending == "" {
					a.store.ClearPanelPending(pendingID)
				}
				fail(w, 409, "panel_apply_failed_restored", "变更失败，已恢复旧持久模板与生成配置；加密备份ID="+id)
			} else {
				fail(w, 503, "panel_recovery_required", "面板恢复无法确认，旧加密快照已保留；检查面板/停止所选入口，备份ID="+id)
			}
			return nil
		}
		if e = a.store.ClearPanelPending(pendingID); e != nil {
			fail(w, 503, "panel_recovery_required", "模板已回读但恢复记录未清除，请检查快照ID="+pendingID)
			return nil
		}
		a.mu.Lock()
		delete(a.panelPreviews, in.PreviewID)
		a.mu.Unlock()
		a.store.AddEvent("info", "panel.persistent_changed", "明确应用/恢复面板持久模板，已回读生成配置；未宣称业务路径通过", target.ExitID)
		write(w, 200, map[string]any{"backup_id": id, "persistent_template_verified": true, "generated_route_verified": !rollback, "existing_inbounds_preserved": true, "business_path_verified": false, "login_password_saved": false, "rollback": rollback})
		return nil
	}
	if rollback {
		e = operation()
	} else {
		e = a.gateway.WithActive(in.ExitID, func(life context.Context, state gateway.State) error {
			if !state.Healthy || !state.ProxyListening {
				return gateway.ErrBusy
			}
			exitLife = life
			return operation()
		})
	}
	if e != nil {
		if errors.Is(e, paneladapter.ErrChanged) {
			fail(w, 409, "panel_changed", e.Error())
		} else if errors.Is(e, gateway.ErrBusy) {
			fail(w, 409, "exit_unavailable", "目标出口不再健康，未应用面板路由")
		} else {
			fail(w, 422, "panel_unavailable", "面板接口/私有备份不可用，未报告成功；最多8份快照")
		}
	}
}

func (a *Server) panelForgetBackup(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "panel_consent", "需明确删除这份私有恢复快照，不会撤销已应用路由")
		return
	}
	if !a.panelSlot(w) {
		return
	}
	defer func() { <-a.panelOperations }()
	if e := a.store.ForgetPanelBackup(r.PathValue("id")); e != nil {
		fail(w, 409, "panel_backup_unavailable", "快照无法验证，未删除")
		return
	}
	a.store.AddEvent("info", "panel.snapshot_forgotten", "管理员明确删除一份已验证面板恢复快照，当前面板路由未改变", "")
	w.WriteHeader(http.StatusNoContent)
}
