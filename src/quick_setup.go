package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var quickRegions = []struct {
	Code, Name string
	Count      int
}{{"JP", "日本", 5}, {"KR", "韩国", 3}, {"US", "美国", 3}}

const quickExitStep = 4
const quickSummaryStep = quickExitStep + 11

type quickSetupRequest struct {
	Domain string `json:"domain"`
}

type QuickRegionResult struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Ready  int    `json:"ready"`
	Target int    `json:"target"`
}

type QuickSetupResult struct {
	Ready   int                 `json:"ready"`
	Target  int                 `json:"target"`
	Regions []QuickRegionResult `json:"regions"`
}

func normalizeQuickDomain(raw string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if domain != "" && (len(domain) > 253 || !validDomain(domain)) {
		return "", fmt.Errorf("请填写域名，例如 node.example.com，无需加 https:// 或端口。")
	}
	return domain, nil
}

func (m *Manager) quickSetupRunning() bool {
	m.quickMu.Lock()
	defer m.quickMu.Unlock()
	return m.quickJob != nil && m.quickJob.View().Status == "running"
}

func apiQuickSetup(m *Manager, domains *DomainRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, 405, map[string]string{"error": "请使用 POST 开始一键配置。"})
			return
		}
		var input quickSetupRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, 2048))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "请求格式有误，请重新打开一键配置。"})
			return
		}
		domain, err := normalizeQuickDomain(input.Domain)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		m.quickMu.Lock()
		if m.quickJob != nil && m.quickJob.View().Status == "running" {
			id := m.quickJob.ID()
			m.quickMu.Unlock()
			writeJSON(w, 202, map[string]any{"job": id, "existing": true})
			return
		}
		labels := []string{"准备 Xray", "域名与证书", "更新节点来源", "准备入口模板"}
		for _, region := range quickRegions {
			for i := 1; i <= region.Count; i++ {
				labels = append(labels, fmt.Sprintf("%s %d", region.Name, i))
			}
		}
		labels = append(labels, "检查节点就绪状态")
		job := m.jobs.New("一键配置 · 日本 5 / 韩国 3 / 美国 3", labels)
		job.SetKind("quick-setup")
		m.quickJob = job
		ctx, cancel := context.WithCancel(context.Background())
		m.quickCancel = cancel
		m.quickMu.Unlock()
		go func() {
			defer cancel()
			m.runQuickSetupContext(ctx, job, domains, domain)
		}()
		writeJSON(w, 202, map[string]string{"job": job.ID()})
	}
}

func (m *Manager) cancelQuickSetup() {
	m.quickMu.Lock()
	defer m.quickMu.Unlock()
	if m.quickCancel != nil {
		m.quickCancel()
	}
}

func apiQuickSetupStop(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, 405, map[string]string{"error": "请使用 POST 停止后台补齐。"})
			return
		}
		m.quickMu.Lock()
		defer m.quickMu.Unlock()
		if m.quickJob == nil || r.URL.Query().Get("id") != m.quickJob.ID() {
			writeJSON(w, 409, map[string]string{"error": "任务已变化，请刷新后重试。"})
			return
		}
		if m.quickCancel != nil {
			m.quickCancel()
		}
		writeJSON(w, 202, map[string]string{"ok": "正在停止后台补齐，已保留连通节点。"})
	}
}

func (m *Manager) quickNative() (*Native, error) {
	previous := currentPanelMode()
	panel, err := openPanel()
	if err != nil || panel.Kind() != "native" {
		panel, err = switchPanelMode("native")
	}
	if err == nil {
		err = panel.OnTunnelsChanged(m.Tunnels())
	}
	if err != nil {
		if restored, restoreErr := switchPanelMode(previous); restoreErr == nil {
			_ = restored.OnTunnelsChanged(m.Tunnels())
		}
		return nil, fmt.Errorf("Xray 暂未启动成功，已尝试恢复原核心：%w", err)
	}
	native, ok := panel.(*Native)
	if !ok {
		return nil, fmt.Errorf("当前核心不是自建 Xray，请稍后再试。")
	}
	panelState.mu.Lock()
	panelState.forced = "native"
	workDir := panelState.workDir
	panelState.mu.Unlock()
	if err := savePanelMode(workDir, "native"); err != nil {
		return nil, fmt.Errorf("保存 Xray 选择失败：%w", err)
	}
	invalidateInbounds()
	return native, nil
}

// Keep certificates dedicated to node publishing. Changing the management
// origin mid-job would interrupt authentication and progress polling.
func (d *DomainRegistry) quickCertificate(domain string, progress func(string)) (RegisteredDomain, error) {
	return d.quickCertificateContext(context.Background(), domain, progress)
}

func (d *DomainRegistry) quickCertificateContext(parent context.Context, domain string, progress func(string)) (RegisteredDomain, error) {
	if !d.operation.TryLock() {
		return RegisteredDomain{}, fmt.Errorf("域名设置正在处理中，请稍后再试。")
	}
	defer d.operation.Unlock()
	d.mu.Lock()
	if d.issuing {
		d.mu.Unlock()
		return RegisteredDomain{}, fmt.Errorf("已有证书正在申请，请等待完成后再试。")
	}
	d.issuing = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.issuing = false; d.mu.Unlock() }()
	for _, choice := range d.Catalog() {
		if !choice.SelfSigned && certificateSupports(choice, domain) {
			progress("找到有效证书，正在自动复用…")
			return d.keepCertificate(choice, domain)
		}
	}
	if !portAvailable(80) {
		return RegisteredDomain{}, fmt.Errorf("申请证书需要使用 80 端口，目前该端口被占用。可先在「域名 / 证书」绑定已有证书，再执行一键配置。")
	}
	args, err := certificateCommandArgs(domainRequest{Domain: domain})
	if err != nil {
		return RegisteredDomain{}, err
	}
	if _, err = exec.LookPath("certbot"); err != nil {
		progress("正在准备自动证书工具，请稍等一下…")
		ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
		err = cmdRun(exec.CommandContext(ctx, "python3", "-B", "/usr/local/lib/miku/maintain.py", "certificate-prepare"))
		cancel()
		if err != nil {
			return RegisteredDomain{}, fmt.Errorf("证书工具暂未准备好，请运行 miku repair 后重试。")
		}
	}
	progress("正在申请证书，请确认域名已解析到本机，且公网 80 端口可以访问…")
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	if err := cmdRun(exec.CommandContext(ctx, "certbot", args...)); err != nil {
		return RegisteredDomain{}, fmt.Errorf("证书暂未申请成功，请检查域名解析和 80 端口后重试；也可以暂不绑定域名，使用 Reality。")
	}
	choice, err := certificateChoice(filepath.Join("/etc/letsencrypt/live", domain, "fullchain.pem"), filepath.Join("/etc/letsencrypt/live", domain, "privkey.pem"), "Certbot")
	if err != nil {
		return RegisteredDomain{}, err
	}
	return d.keepCertificate(choice, domain)
}

func quickInboundSpec(domain string, cert RegisteredDomain) NewInboundSpec {
	spec := NewInboundSpec{Protocol: "vless", Network: "tcp", Security: "reality", Vision: true, Remark: "Miku 一键 · Reality"}
	if domain != "" {
		spec.Security, spec.ServerName = "tls", domain
		spec.CertFile, spec.KeyFile = cert.CertFile, cert.KeyFile
		spec.Remark = "Miku 一键 · " + domain
	}
	return spec
}

func (n *Native) quickTemplate(spec NewInboundSpec, tunnels []*Tunnel) (int, error) {
	n.mu.Lock()
	for _, ib := range n.store.Inbounds {
		if !ib.Enable || ib.BoundTo != "" || ib.AutoExitOrder != 0 || ib.Remark != spec.Remark || ib.Protocol != "vless" || ib.netOrTCP() != "tcp" || ib.securityOrNone() != spec.Security {
			continue
		}
		if len(ib.Clients) != 1 || !ib.Clients[0].Enable || ib.Clients[0].BoundTo != "" || ib.Clients[0].Flow != "xtls-rprx-vision" {
			continue
		}
		if spec.Security == "tls" && (ib.TLS == nil || ib.TLS.SelfSigned || ib.TLS.ServerName != spec.ServerName || ib.TLS.CertFile != spec.CertFile || ib.TLS.KeyFile != spec.KeyFile) {
			continue
		}
		if spec.Security == "reality" && ib.Reality == nil {
			continue
		}
		id := ib.ID
		n.mu.Unlock()
		return id, nil
	}
	n.mu.Unlock()
	if id, changed, err := n.reconfigureQuickTemplate(spec, tunnels); changed || err != nil {
		return id, err
	}
	created, err := n.CreateInbound(spec, tunnels)
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// Targets describe missing successful exits, independently of the current
// source size. Even a country with no candidates remains eligible for refresh.
func (m *Manager) quickTargets(job *Job, templateID int) []quickTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	existing := map[string][]*Tunnel{}
	for _, t := range m.tunnels {
		if t.TemplateID == templateID && t.Status != "stopped" && !t.quickCandidate.Load() {
			code := t.preferredRegion()
			existing[code] = append(existing[code], t)
		}
	}
	room := linkSubnetCapacity - len(m.tunnels)
	if m.maxSlots > 0 && m.maxSlots-len(m.tunnels) < room {
		room = m.maxSlots - len(m.tunnels)
	}
	var targets []quickTarget
	step := quickExitStep
	for _, region := range quickRegions {
		for i := 0; i < region.Count; i++ {
			if i < len(existing[region.Code]) {
				t := existing[region.Code][i]
				if t.Status == "up" && t.EntryReady {
					job.Set(step, "ok", fmt.Sprintf("保留现有出口 #%d", t.Slot))
				} else {
					job.Set(step, "failed", fmt.Sprintf("出口 #%d 已存在，仍在连接或修复入口，请查看出口卡片", t.Slot))
				}
			} else if room > 0 {
				targets = append(targets, quickTarget{code: region.Code, step: step})
				room--
				job.Set(step, "running", region.Name+"正在后台筛选，持续尝试直到补齐可用节点…")
			} else {
				job.Set(step, "failed", "可用出口槽位不足，请先停止不需要的出口后再补齐。")
			}
			step++
		}
	}
	return targets
}

func (m *Manager) runQuickSetup(job *Job, domains *DomainRegistry, domain string) {
	m.runQuickSetupContext(context.Background(), job, domains, domain)
}

func (m *Manager) runQuickSetupContext(ctx context.Context, job *Job, domains *DomainRegistry, domain string) {
	defer func() {
		m.quickMu.Lock()
		if m.quickJob == job {
			m.quickCancel = nil
		}
		m.quickMu.Unlock()
	}()
	defer job.Finish()
	canceled := func() bool {
		if ctx.Err() == nil {
			return false
		}
		job.failUnfinished("后台补齐已停止，已保留连通节点。")
		return true
	}
	if canceled() {
		return
	}
	// Fetch in parallel with core/certificate preparation. Partial validated
	// pools wake the race immediately; a slow source never gates the first exit.
	job.Set(2, "running", "后台更新来源，先到的可用节点立即开始连接…")
	go func() { _, _ = m.refreshNodesContext(ctx) }()
	fail := func(step int, err error) {
		job.Set(step, "failed", firstLine(err.Error()))
		job.failUnfinished("前一步暂未完成，请处理提示后再试。")
	}
	job.Set(0, "running", "正在准备 Xray 核心…")
	native, err := m.quickNative()
	if err != nil {
		fail(0, err)
		return
	}
	if canceled() {
		return
	}
	job.Set(0, "ok", "Xray 已就绪")
	var cert RegisteredDomain
	if domain == "" {
		job.Set(1, "ok", "使用 Reality，无需域名和证书")
	} else {
		job.Set(1, "running", "正在检查域名证书…")
		cert, err = domains.quickCertificateContext(ctx, domain, func(detail string) { job.Set(1, "running", detail) })
		if err != nil {
			fail(1, err)
			return
		}
		job.Set(1, "ok", domain+" 证书已就绪")
	}
	if canceled() {
		return
	}
	job.Set(2, "ok", "来源在后台持续更新，使用已到达节点立即竞速")
	job.Set(3, "running", "正在准备加密入口…")
	templateID, err := native.quickTemplate(quickInboundSpec(domain, cert), m.Tunnels())
	if err != nil {
		fail(3, err)
		return
	}
	job.Set(3, "ok", protocolStack("vless", "tcp", quickInboundSpec(domain, cert).Security, true, false))
	if canceled() {
		return
	}
	targets := m.quickTargets(job, templateID)
	m.updateQuickResult(job, templateID)
	job.Set(quickSummaryStep, "running", "正在并行筛选出口，优先保留日本 5、韩国 3、美国 3 个可用节点…")
	if len(targets) > 0 {
		m.runQuickRace(job, targets, templateID, quickRaceOps{ctx: ctx})
	}
	ready, pending := 0, 0
	counts := map[string]int{}
	for _, t := range m.visibleTunnels() {
		if t.TemplateID == templateID && t.Status != "stopped" {
			if t.Status == "up" && t.EntryReady {
				ready++
				counts[t.preferredRegion()]++
			} else {
				pending++
			}
		}
	}
	result := QuickSetupResult{Ready: ready, Target: 11}
	for _, region := range quickRegions {
		result.Regions = append(result.Regions, QuickRegionResult{Code: region.Code, Name: region.Name, Ready: counts[region.Code], Target: region.Count})
	}
	job.SetQuickResult(result)
	status, detail := "ok", fmt.Sprintf("%d 个节点已就绪，订阅地址和全部节点链接已准备好。", ready)
	if ready < 11 {
		status, detail = "failed", fmt.Sprintf("%d / 11 个已就绪，已保留可用节点；需要更多可到「出口与节点 → 新建出口」添加。", ready)
		if ctx.Err() != nil {
			detail = fmt.Sprintf("后台补齐已停止，已保留 %d / 11 个可用节点。", ready)
		}
		if pending > 0 {
			detail += fmt.Sprintf(" %d 个已有出口仍在自动连接。", pending)
		}
	}
	job.Set(quickSummaryStep, status, detail)
	invalidateInbounds()
	m.notifyPanel()
}

// Publish partial results while the job is running, for both the browser and
// the VPS command. Readiness always requires the encrypted entry as well.
func (m *Manager) updateQuickResult(job *Job, templateID int) {
	result := QuickSetupResult{Target: 11}
	counts := map[string]int{}
	m.mu.RLock()
	for _, t := range m.tunnels {
		if t.TemplateID == templateID && !t.quickCandidate.Load() && t.Status == "up" && t.EntryReady {
			result.Ready++
			counts[t.preferredRegion()]++
		}
	}
	m.mu.RUnlock()
	for _, region := range quickRegions {
		result.Regions = append(result.Regions, QuickRegionResult{Code: region.Code, Name: region.Name, Ready: counts[region.Code], Target: region.Count})
	}
	job.SetQuickResult(result)
}
