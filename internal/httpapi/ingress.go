package httpapi

import (
	"net/http"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ingress"
)

func (a *Server) ingressSources(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	write(w, 200, ingress.Sources())
}
func (a *Server) ingressInspect(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Source      string `json:"source"`
		ConfirmRead bool   `json:"confirm_read"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.ConfirmRead {
		fail(w, 400, "confirm_read_required", "需要明确确认读取核心配置；不会读取面板数据库或单独打开私钥文件")
		return
	}
	source, err := ingress.FindSource(input.Source)
	if err != nil {
		fail(w, 400, "source_invalid", "请选择固定核心配置来源；不接受目录或文件路径")
		return
	}
	select {
	case a.ingressSlot <- struct{}{}:
		defer func() { <-a.ingressSlot }()
	default:
		fail(w, 429, "ingress_busy", "核心只读检测正在进行")
		return
	}
	a.mu.Lock()
	if !a.lastIngress.IsZero() && time.Since(a.lastIngress) < 3*time.Second {
		a.mu.Unlock()
		fail(w, 429, "ingress_rate_limited", "请稍后再检测，避免重复读取配置")
		return
	}
	a.lastIngress = time.Now()
	a.mu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	report, err := a.inspectIngress(source)
	if err != nil {
		fail(w, 422, "ingress_unavailable", "配置不可读、含符号链接/非安全权限，或不是受支持的严格JSON。不会放宽权限或回显原始配置；请使用授权安装助手检测。")
		return
	}
	if report.Source != source || !ingress.ValidSHA(report.SHA256) || !report.ReadOnly || report.Applied || report.CoreValidated {
		fail(w, 500, "ingress_report_invalid", "检测返回的来源或状态不匹配")
		return
	}
	write(w, 200, report)
}
