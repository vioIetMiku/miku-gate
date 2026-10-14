package httpapi

import (
	"encoding/base64"
	"errors"
	"mikugate/internal/domain"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *Server) clients(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	entries, err := a.store.ClientEntries()
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	write(w, 200, entries)
}
func (a *Server) subscriptions(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	entries, err := a.store.ListSubscriptions()
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	write(w, 200, entries)
}
func (a *Server) subscriptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrSubscriptionCapacity):
		fail(w, 409, "subscription_capacity", "最多64份订阅，请撤销并移除旧记录后再创建")
	case errors.Is(err, domain.ErrSubscriptionUnavailable):
		fail(w, 422, "subscription_scope_unavailable", "所选入口未保存或无法读取客户端材料")
	case errors.Is(err, domain.ErrConflict):
		fail(w, 409, "subscription_active", "请先撤销订阅，再移除记录")
	default:
		a.runtimeError(w, err)
	}
}
func (a *Server) subscriptionCreate(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		domain.SubscriptionInput
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认订阅链接持有人可以获取所选入口凭据")
		return
	}
	if err := input.SubscriptionInput.Validate(); err != nil {
		fail(w, 400, "invalid_subscription", err.Error())
		return
	}
	item, token, err := a.store.CreateSubscription(r.Context(), input.SubscriptionInput)
	if err != nil {
		a.subscriptionError(w, err)
		return
	}
	// The raw bearer is delivered once; later views contain metadata only.
	write(w, 201, map[string]any{"subscription": item.View(time.Now()), "token": token, "formats": []string{"base64", "uri"}})
}
func subscriptionConfirm(w http.ResponseWriter, r *http.Request) bool {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return false
	}
	if !in.Confirm {
		fail(w, 400, "confirm_required", "请明确确认订阅操作")
		return false
	}
	return true
}
func (a *Server) subscriptionRevoke(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !subscriptionConfirm(w, r) {
		return
	}
	item, err := a.store.RevokeSubscription(r.Context(), r.PathValue("id"))
	if err != nil {
		a.subscriptionError(w, err)
		return
	}
	write(w, 200, item.View(time.Now()))
}
func (a *Server) subscriptionDelete(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !subscriptionConfirm(w, r) {
		return
	}
	if err := a.store.DeleteSubscription(r.Context(), r.PathValue("id")); err != nil {
		a.subscriptionError(w, err)
		return
	}
	w.WriteHeader(204)
}

// This route uses its own bounded bearer authorization, never an admin cookie.
// The normal Host/Origin/direct-HTTPS guard still wraps it.
func (a *Server) subscriptionFetch(w http.ResponseWriter, r *http.Request) {
	select {
	case a.subscriptionSlots <- struct{}{}:
		defer func() { <-a.subscriptionSlots }()
	default:
		fail(w, 429, "subscription_busy", "订阅请求繁忙，请稍后重试")
		return
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	format := "base64"
	if queryErr != nil || len(r.URL.RawQuery) > 64 || len(query) > 1 || (len(query) == 1 && (len(query["format"]) != 1 || (query.Get("format") != "base64" && query.Get("format") != "uri"))) {
		fail(w, 400, "invalid_subscription_format", "仅支持base64或uri格式")
		return
	}
	if len(query) == 1 {
		format = query.Get("format")
	}
	item, err := a.store.SubscriptionForToken(r.PathValue("token"))
	unavailable := func() { fail(w, 404, "subscription_unavailable", "订阅不可用、过期或已撤销") }
	if err != nil {
		unavailable()
		return
	}
	links := []string{}
	revisions := map[string]string{}
	for _, id := range item.ExitIDs {
		if r.Context().Err() != nil {
			return
		}
		material, _, ok, revision, err := a.store.ManagedSnapshot(id)
		if err != nil || !ok {
			unavailable()
			return
		}
		client, err := material.ClientBundleForDevice(item.DeviceIDs[id])
		if err != nil {
			unavailable()
			return
		}
		exit, _, err := a.store.ExitForRuntime(id)
		if err != nil {
			unavailable()
			return
		}
		link, err := url.Parse(client.URI)
		if err != nil {
			unavailable()
			return
		}
		label := "共享凭据"
		for _, d := range material.Devices {
			if d.ID == item.DeviceIDs[id] {
				label = d.Name
			}
		}
		link.Fragment = exit.Name + " · " + material.Request.Core
		if item.DeviceIDs[id] != "" {
			link.Fragment = exit.Name + " · " + label + " · " + material.Request.Core
		}
		links = append(links, link.String())
		revisions[id] = revision
	}
	// Recheck after material reads; already in-flight writes may still finish.
	for _, id := range item.ExitIDs {
		_, _, ok, revision, err := a.store.ManagedSnapshot(id)
		if err != nil || !ok || revision != revisions[id] {
			unavailable()
			return
		}
	}
	if _, err = a.store.SubscriptionForToken(r.PathValue("token")); err != nil {
		unavailable()
		return
	}
	body := strings.Join(links, "\n") + "\n"
	if format == "base64" {
		body = base64.StdEncoding.EncodeToString([]byte(body)) + "\n"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="miku-subscription.txt"`)
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		w.Write([]byte(body))
	}
}
