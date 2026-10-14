package httpapi

import (
	"mikugate/internal/domain"
	"mikugate/internal/store"
	"net/http"
	"strings"
)

func sessionAllows(s domain.Session, id string) bool {
	if s.Role == "admin" {
		return true
	}
	for _, allowed := range s.ExitIDs {
		if allowed == id {
			return true
		}
	}
	return false
}
func allowedRequest(s domain.Session, r *http.Request) bool {
	if s.Role == "admin" {
		return true
	}
	if s.Role != "viewer" && s.Role != "operator" {
		return false
	}
	path := r.URL.Path
	if path == "/api/auth/logout" || path == "/api/auth/password" {
		return true
	}
	if r.Method == "GET" {
		switch path {
		case "/api/auth/session", "/api/overview", "/api/exits", "/api/nodes", "/api/events", "/api/history", "/api/diagnostics", "/api/management", "/api/vpn-executor", "/api/executor":
			return true
		}
	}
	if s.Role == "operator" && r.Method == "POST" && strings.HasPrefix(path, "/api/tasks/") && strings.HasSuffix(path, "/cancel") {
		return true
	}
	if path == "/api/tasks" && s.Role == "operator" {
		return true
	}
	if strings.HasPrefix(path, "/api/exits/") {
		id := r.PathValue("id")
		if !sessionAllows(s, id) {
			return false
		}
		suffix := strings.TrimPrefix(path, "/api/exits/"+id)
		if r.Method == "GET" {
			switch suffix {
			case "/runtime", "/vpn", "/ingress", "/ingress/runtime", "/recovery", "/standby", "/vpn/transport", "/network-plan":
				return true
			}
		}
		if r.Method == "POST" && s.Role == "operator" {
			switch suffix {
			case "/start", "/stop", "/probe", "/ingress/start", "/ingress/stop":
				return true
			}
		}
	}
	return false
}
func (a *Server) accountList(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	v, err := a.store.Accounts()
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, v)
}
func (a *Server) accountCreate(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		store.Account
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "需确认账号权限和出口范围")
		return
	}
	if !a.operationGate(w) {
		return
	}
	defer func() { <-a.operationsSlot }()
	password, err := a.store.CreateAccount(in.Account)
	if err != nil {
		fail(w, 400, "account_invalid", err.Error())
		return
	}
	write(w, 201, map[string]any{"account": in.Account, "password": password, "shown_once": true})
}
func (a *Server) accountUpdate(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in struct {
		Role    string   `json:"role"`
		ExitIDs []string `json:"exit_ids"`
		Confirm bool     `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "需确认权限变更及旧会话退出")
		return
	}
	if err := a.store.ChangeAccount(store.Account{Username: r.PathValue("username"), Role: in.Role, ExitIDs: in.ExitIDs}, s.Username, false); err != nil {
		fail(w, 400, "account_rejected", err.Error())
		return
	}
	w.WriteHeader(204)
}
func (a *Server) accountDelete(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if err := a.store.ChangeAccount(store.Account{Username: r.PathValue("username")}, s.Username, true); err != nil {
		fail(w, 400, "account_rejected", err.Error())
		return
	}
	w.WriteHeader(204)
}
