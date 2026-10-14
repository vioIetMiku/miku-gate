package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/fleet"
	"mikugate/internal/gateway"
)

func (a *Server) fleetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agent/tokens", a.auth(a.agentTokens))
	mux.HandleFunc("POST /api/agent/tokens", a.auth(a.agentTokenCreate))
	mux.HandleFunc("DELETE /api/agent/tokens/{id}", a.auth(a.agentTokenRevoke))
	mux.HandleFunc("GET /api/fleet", a.auth(a.fleetList))
	mux.HandleFunc("POST /api/fleet", a.auth(a.fleetSave))
	mux.HandleFunc("DELETE /api/fleet/{id}", a.auth(a.fleetDelete))
	mux.HandleFunc("POST /api/fleet/{id}/state", a.auth(a.fleetState))
	mux.HandleFunc("POST /api/fleet/{id}/command", a.auth(a.fleetCommand))
	mux.HandleFunc("GET /agent/v1/state", a.agent(a.agentState, false))
	mux.HandleFunc("POST /agent/v1/exits/{id}/{action}", a.agent(a.agentCommand, true))
}
func (a *Server) remoteGate(w http.ResponseWriter) bool {
	select {
	case a.remoteSlots <- struct{}{}:
		return true
	default:
		fail(w, 429, "agent_busy", "远端操作达到并发上限2")
		return false
	}
}
func (a *Server) agentTokens(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	v, err := a.store.AgentTokens()
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, map[string]any{"tokens": v, "endpoint_enabled": a.publicEndpoint != nil && r.TLS != nil, "transport": "direct verified HTTPS", "max_tokens": 32})
}
func (a *Server) agentTokenCreate(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		Name      string    `json:"name"`
		Role      string    `json:"role"`
		ExitIDs   []string  `json:"exit_ids"`
		ExpiresAt time.Time `json:"expires_at"`
		Confirm   bool      `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm || a.publicEndpoint == nil || r.TLS == nil {
		fail(w, 400, "agent_https_required", "需已配置直接HTTPS并明确授权令牌范围")
		return
	}
	t, secret, err := a.store.CreateAgentToken(domain.AgentToken{Name: in.Name, Role: in.Role, ExitIDs: in.ExitIDs, ExpiresAt: in.ExpiresAt})
	if err != nil {
		fail(w, 400, "token_rejected", err.Error())
		return
	}
	write(w, 201, map[string]any{"token": t, "secret": secret, "shown_once": true})
}
func (a *Server) agentTokenRevoke(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.RevokeAgentToken(r.PathValue("id")); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) agent(fn func(http.ResponseWriter, *http.Request, domain.AgentToken), writeAccess bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.publicEndpoint == nil || r.TLS == nil {
			fail(w, 403, "agent_https_required", "Agent只接受已配置的直接HTTPS")
			return
		}
		if !a.remoteGate(w) {
			return
		}
		defer func() { <-a.remoteSlots }()
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(w, 401, "agent_unauthenticated", "需独立Agent令牌")
			return
		}
		token, err := a.store.AgentToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			fail(w, 401, "agent_unauthenticated", "Agent令牌无效或已过期/撤销")
			return
		}
		if writeAccess && token.Role != "operator" {
			fail(w, 403, "agent_read_only", "该令牌仅有只读权限")
			return
		}
		if id := r.PathValue("id"); id != "" {
			allowed := false
			for _, scope := range token.ExitIDs {
				if scope == id {
					allowed = true
				}
			}
			if !allowed {
				fail(w, 403, "agent_scope", "该出口不在令牌授权范围")
				return
			}
		}
		fn(w, r, token)
	}
}
func (a *Server) agentState(w http.ResponseWriter, r *http.Request, t domain.AgentToken) {
	instance, err := a.store.InstanceID()
	if err != nil {
		a.internal(w, err)
		return
	}
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return
	}
	scope := map[string]bool{}
	for _, id := range t.ExitIDs {
		scope[id] = true
	}
	state := fleet.State{Version: domain.Version, Instance: instance, Role: t.Role, Exits: []domain.Exit{}, Runtimes: []gateway.State{}, ObservedAt: time.Now().UTC()}
	for _, exit := range exits {
		if scope[exit.ID] {
			state.Exits = append(state.Exits, exit)
			if a.gateway != nil {
				if runtime, ok := a.gateway.Snapshot(exit.ID); ok {
					state.Runtimes = append(state.Runtimes, runtime)
				}
			}
		}
	}
	write(w, 200, state)
}
func (a *Server) agentCommand(w http.ResponseWriter, r *http.Request, t domain.AgentToken) {
	action := r.PathValue("action")
	if action != "start" && action != "stop" && action != "probe" {
		fail(w, 400, "agent_operation_invalid", "Agent仅允许启动、停止和检测授权出口")
		return
	}
	if err := a.operateExit(r.Context(), action, r.PathValue("id")); err != nil {
		fail(w, 409, "agent_operation_failed", err.Error())
		return
	}
	_ = a.store.AddEvent("info", "agent."+action, "限定Agent令牌已执行出口操作", r.PathValue("id"))
	w.WriteHeader(204)
}
func (a *Server) fleetList(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	v, err := a.store.FleetHosts()
	if err != nil {
		a.internal(w, err)
		return
	}
	for i := range v {
		v[i].Token = ""
	}
	write(w, 200, v)
}
func (a *Server) fleetSave(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		Name         string `json:"name"`
		Origin       string `json:"origin"`
		Token        string `json:"token"`
		CA           string `json:"ca_pem"`
		AllowPrivate bool   `json:"allow_private"`
		Confirm      bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "请确认这是你授权管理的远端，CA信任和可选私网访问")
		return
	}
	h := domain.FleetHost{Name: in.Name, Origin: in.Origin, Token: in.Token, CA: in.CA, AllowPrivate: in.AllowPrivate}
	if err := fleet.Validate(h); err != nil {
		fail(w, 400, "fleet_invalid", err.Error())
		return
	}
	h, err := a.store.SaveFleetHost(h)
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 201, h)
}
func (a *Server) fleetDelete(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if err := a.store.DeleteFleetHost(r.PathValue("id")); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) fleetState(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !a.remoteGate(w) {
		return
	}
	defer func() { <-a.remoteSlots }()
	h, err := a.store.FleetHost(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "fleet_not_found", "远端不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	state, err := fleet.Inspect(ctx, h)
	if err != nil {
		fail(w, 502, "fleet_unavailable", err.Error())
		return
	}
	write(w, 200, map[string]any{"state": state, "received_at": time.Now().UTC(), "freshness_seconds": 30})
}
func (a *Server) fleetCommand(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var in struct {
		ExitID  string `json:"exit_id"`
		Action  string `json:"action"`
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirm || !regexp.MustCompile(`^[A-Za-z0-9_-]{16}$`).MatchString(in.ExitID) || (in.Action != "start" && in.Action != "stop" && in.Action != "probe") {
		fail(w, 400, "fleet_command_invalid", "需确认授权出口的启动/停止/检测")
		return
	}
	if !a.remoteGate(w) {
		return
	}
	defer func() { <-a.remoteSlots }()
	h, err := a.store.FleetHost(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "fleet_not_found", "远端不存在")
		return
	}
	_, err = fleet.Request(r.Context(), h, "POST", "/agent/v1/exits/"+in.ExitID+"/"+in.Action)
	if err != nil {
		fail(w, 502, "fleet_command_failed", err.Error())
		return
	}
	_ = a.store.AddEvent("info", "fleet.command", "已在授权远端执行出口操作", "")
	w.WriteHeader(204)
}
