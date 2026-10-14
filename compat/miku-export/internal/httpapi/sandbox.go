package httpapi

import (
	"errors"
	"net/http"

	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/netexec"
)

func (a *Server) executorStatus(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	status := map[string]any{"configured": a.sandbox != nil, "reachable": false, "mode": "quarantine-only", "vpn_available": false, "proxy_available": false, "message": "未启用隔离 IPC；需要管理员显式授权执行器并用 --sandbox-ipc 启动控制台。"}
	if a.sandbox != nil {
		caps, err := a.sandbox.Capabilities(r.Context())
		if err != nil {
			status["message"] = "已配置 IPC，但当前无法验证执行器；不代表任何隔离或隧道已生效。"
		} else {
			status["reachable"] = true
			status["capabilities"] = caps
			status["message"] = "已验证本机 root 执行器身份，仅授权隔离操作；仍无 VPN / 代理数据面。"
		}
	}
	write(w, http.StatusOK, status)
}
func (a *Server) requireExit(w http.ResponseWriter, id string) bool {
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return false
	}
	for _, exit := range exits {
		if exit.ID == id {
			return true
		}
	}
	a.storeError(w, domain.ErrNotFound)
	return false
}
func (a *Server) acquireSandbox(w http.ResponseWriter) bool {
	select {
	case a.sandboxSlot <- struct{}{}:
		return true
	default:
		fail(w, 409, "sandbox_busy", "已有隔离/删除操作运行中，请先等待并查询实际状态。")
		return false
	}
}
func (a *Server) requireSandbox(w http.ResponseWriter) bool {
	if a.sandbox == nil {
		fail(w, 503, "sandbox_ipc_disabled", "未显式接入隔离 IPC，不执行特权操作。")
		return false
	}
	return true
}
func (a *Server) sandboxInspect(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !a.requireExit(w, r.PathValue("id")) || !a.requireSandbox(w) {
		return
	}
	result, err := a.sandbox.Inspect(r.Context(), r.PathValue("id"))
	if err != nil {
		a.sandboxError(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
func (a *Server) sandboxPrepare(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	a.sandboxChange(w, r, "prepare")
}
func (a *Server) sandboxCleanup(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	a.sandboxChange(w, r, "cleanup")
}
func (a *Server) sandboxChange(w http.ResponseWriter, r *http.Request, operation string) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirmation_required", "必须显式确认本次隔离操作；它不会启动 VPN。")
		return
	}
	if !a.acquireSandbox(w) {
		return
	}
	defer func() { <-a.sandboxSlot }()
	id := r.PathValue("id")
	if !a.requireExit(w, id) || !a.requireSandbox(w) {
		return
	}
	if err := a.store.AddEvent("info", "sandbox."+operation+"_requested", "已显式请求隔离操作；不是 VPN 启动。", id); err != nil {
		a.internal(w, err)
		return
	}
	var result netexec.Result
	var err error
	if operation == "prepare" {
		result, err = a.sandbox.Prepare(r.Context(), id)
	} else {
		result, err = a.sandbox.Cleanup(r.Context(), id)
	}
	if err != nil {
		_ = a.store.AddEvent("warning", "sandbox."+operation+"_failed", "隔离操作未确认完成；请查询实际状态，不自动重试。", id)
		a.sandboxError(w, err)
		return
	}
	if err = a.store.AddEvent("info", "sandbox."+operation+"_completed", "隔离操作完成，实际阶段："+result.Phase+"；未建立 VPN 或代理。", id); err != nil {
		a.internal(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
func (a *Server) sandboxError(w http.ResponseWriter, err error) {
	var rpc *executoripc.RPCError
	if errors.As(err, &rpc) {
		status := http.StatusConflict
		if rpc.Code == "operation_failed" || rpc.Code == "unverified" {
			status = http.StatusBadGateway
		}
		if rpc.Code == "timeout" || rpc.Code == "canceled" {
			status = http.StatusGatewayTimeout
		}
		fail(w, status, "sandbox_"+rpc.Code, rpc.Message)
		return
	}
	fail(w, http.StatusServiceUnavailable, "sandbox_ipc_unavailable", "无法验证或连接本机隔离执行器；不自动重试，可能需查询实际状态。")
}
