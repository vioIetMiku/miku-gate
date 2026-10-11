package httpapi

import (
	"context"
	"encoding/json"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/paneladapter"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type panelFixture struct {
	snapshot paneladapter.Snapshot
	fail     bool
	saves    int
}

func (f *panelFixture) Login(context.Context, string, string, string) error { return nil }
func (f *panelFixture) Read(context.Context) (paneladapter.Snapshot, error) { return f.snapshot, nil }
func (f *panelFixture) Save(_ context.Context, value paneladapter.Snapshot) error {
	f.saves++
	if f.fail {
		f.fail = false
		return paneladapter.ErrPanel
	}
	var original, next map[string]json.RawMessage
	json.Unmarshal(f.snapshot.Rendered, &original)
	json.Unmarshal(value.Template, &next)
	next["inbounds"] = original["inbounds"]
	raw, _ := json.Marshal(next)
	f.snapshot = paneladapter.Snapshot{Template: value.Template, Rendered: raw, TestURL: value.TestURL}
	return nil
}
func TestPanelAuthenticatedPersistentApplyRollbackAndEncryptedSnapshot(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	hash, _ := security.HashPassword("admin-fixture")
	s.InitAccount("admin", hash)
	s.CreateSession("admin", "panel-session", "csrf", time.Now().Add(time.Hour))
	exit, pass, e := s.CreateExit(domain.ExitInput{Name: "panel exit", Country: "JP"})
	if e != nil {
		t.Fatal(e)
	}
	s.SaveUpstream(exit.ID, domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443})
	manager, e := gateway.New(t.Context(), s, gateway.Options{HealthInterval: time.Hour, Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (gateway.Session, error) {
		return &apiSession{target: "127.0.0.1:9"}, nil
	}, Listen: func(ctx context.Context, _, _ string) (net.Listener, error) {
		return (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close(context.Background())
	if _, e = manager.Start(t.Context(), exit.ID); e != nil {
		t.Fatal(e)
	}
	template, _ := paneladapter.Canonical([]byte(`{"inbounds":[],"outbounds":[{"tag":"direct","protocol":"freedom"}],"routing":{"rules":[]}}`))
	rendered, _ := paneladapter.Canonical([]byte(`{"inbounds":[{"tag":"existing-entry","protocol":"vless","port":28443,"listen":"127.0.0.1","settings":{"clients":[{"id":"private-client-identity"}]},"streamSettings":{"network":"tcp","security":"tls"}}],"outbounds":[{"tag":"direct","protocol":"freedom"}],"routing":{"rules":[]}}`))
	fixture := &panelFixture{snapshot: paneladapter.Snapshot{Template: template, Rendered: rendered, TestURL: "https://fixture.example/"}}
	factory := func(paneladapter.Endpoint) (paneladapter.Runtime, error) { return fixture, nil }
	handler, e := NewWithOptions(s, nil, Options{PanelFactory: factory, Gateway: manager})
	if e != nil {
		t.Fatal(e)
	}
	request := func(path string, body any, auth, csrf bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.AddCookie(&http.Cookie{Name: "miku_session", Value: "panel-session"})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := map[string]any{"endpoint": paneladapter.Endpoint{Port: 2053, Base: "/", Mode: "current"}, "username": "admin", "password": "private-panel-password", "confirm": true}
	if request("/api/host/panel/inspect", login, false, false).Code != 401 || request("/api/host/panel/inspect", login, true, false).Code != 403 {
		t.Fatal("unprotected panel login")
	}
	w := request("/api/host/panel/inspect", login, true, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-") {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview struct {
		ID       string `json:"preview_id"`
		Template string `json:"template_sha256"`
		Rendered string `json:"rendered_sha256"`
		Report   struct {
			Inbounds []struct {
				ID string `json:"id"`
			} `json:"inbounds"`
		} `json:"report"`
	}
	json.Unmarshal(w.Body.Bytes(), &preview)
	apply := map[string]any{"preview_id": preview.ID, "template_sha256": preview.Template, "rendered_sha256": preview.Rendered, "exit_id": exit.ID, "inbound_id": preview.Report.Inbounds[0].ID, "exit_password": pass, "confirm": true}
	planned := request("/api/host/panel/plan", apply, true, true)
	if planned.Code != 200 || strings.Contains(planned.Body.String(), pass) || strings.Contains(planned.Body.String(), "private-") {
		t.Fatal("safe plan", planned.Code, planned.Body.String())
	}
	var plannedBody struct {
		Plan struct {
			Candidate string `json:"candidate_sha256"`
		} `json:"plan"`
	}
	json.Unmarshal(planned.Body.Bytes(), &plannedBody)
	apply["candidate_sha256"] = plannedBody.Plan.Candidate
	fixture.fail = true
	failed := request("/api/host/panel/apply", apply, true, true)
	if failed.Code != 409 || !strings.Contains(failed.Body.String(), "panel_apply_failed_restored") || !fixture.snapshot.Same(paneladapter.Snapshot{Template: template, Rendered: rendered, TestURL: "https://fixture.example/"}) {
		t.Fatal("failed apply didn't restore old pair", failed.Code, failed.Body.String())
	}
	w = request("/api/host/panel/apply", apply, true, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"persistent_template_verified":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		ID string `json:"backup_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	// Re-login in a fresh API instance to avoid the intentional 3-second limiter.
	handler, e = NewWithOptions(s, nil, Options{PanelFactory: factory, Gateway: manager})
	if e != nil {
		t.Fatal(e)
	}
	w = request("/api/host/panel/inspect", login, true, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &preview)
	rollback := map[string]any{"preview_id": preview.ID, "template_sha256": preview.Template, "rendered_sha256": preview.Rendered, "backup_id": result.ID, "confirm": true}
	w = request("/api/host/panel/rollback", rollback, true, true)
	if w.Code != 200 || !fixture.snapshot.Same(paneladapter.Snapshot{Template: template, Rendered: rendered, TestURL: "https://fixture.example/"}) {
		t.Fatal("panel paired rollback", w.Code, w.Body.String())
	}
	ids, e := s.PanelBackupIDs()
	if e != nil || len(ids) != 3 {
		t.Fatal("encrypted panel backup missing", e)
	}
}
