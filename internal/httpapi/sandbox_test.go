package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/netexec"
	"mikugate/internal/store"
)

type apiSandbox struct {
	instance         string
	mu               sync.Mutex
	phase            string
	calls            int
	failure          error
	entered, release chan struct{}
}

func (f *apiSandbox) Capabilities(context.Context) (executoripc.Capabilities, error) {
	return executoripc.Capabilities{Version: 1, InstanceID: f.instance, Mode: "quarantine-only", SandboxAvailable: true}, f.failure
}
func (f *apiSandbox) state(ctx context.Context, id, op string) (netexec.Result, error) {
	f.mu.Lock()
	f.calls++
	failure := f.failure
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if failure != nil {
		return netexec.Result{}, failure
	}
	if op == "prepare" && entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return netexec.Result{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if op == "prepare" {
		f.phase = "quarantined"
	}
	if op == "cleanup" {
		f.phase = "absent"
	}
	if f.phase == "" {
		f.phase = "absent"
	}
	plan, _ := netexec.BuildPlan(f.instance, id)
	return netexec.Result{Plan: plan, Phase: f.phase, QuarantineVerified: f.phase == "quarantined"}, nil
}
func (f *apiSandbox) Inspect(c context.Context, id string) (netexec.Result, error) {
	return f.state(c, id, "inspect")
}
func (f *apiSandbox) Prepare(c context.Context, id string) (netexec.Result, error) {
	return f.state(c, id, "prepare")
}
func (f *apiSandbox) Cleanup(c context.Context, id string) (netexec.Result, error) {
	return f.state(c, id, "cleanup")
}

type sandboxFixture struct {
	s       *store.Store
	backend *apiSandbox
	handler http.Handler
	exit    domain.Exit
}

func newSandboxFixture(t *testing.T, enabled bool) sandboxFixture {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.InitAccount("admin", "unused-hash"); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateSession("admin", "test-session", "test-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	exit, _, err := s.CreateExit(domain.ExitInput{Name: "IPC测试", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	instance, _ := s.InstanceID()
	backend := &apiSandbox{instance: instance}
	options := Options{}
	if enabled {
		options.Sandbox = backend
	}
	handler, err := NewWithOptions(s, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	return sandboxFixture{s: s, backend: backend, handler: handler, exit: exit}
}
func (f sandboxFixture) request(method, path, body string, auth, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://localhost:3939/api"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.AddCookie(&http.Cookie{Name: "miku_session", Value: "test-session"})
	}
	if csrf {
		req.Header.Set("X-CSRF-Token", "test-csrf")
	}
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	return out
}
func TestSandboxHTTPRequiresExplicitEnableAndConfirmation(t *testing.T) {
	f := newSandboxFixture(t, false)
	path := "/exits/" + f.exit.ID + "/sandbox"
	if w := f.request("GET", "/executor", "", false, false); w.Code != 401 {
		t.Fatal("anonymous executor query accepted")
	}
	if w := f.request("GET", "/executor", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatal(w.Body.String())
	}
	if w := f.request("POST", path+"/prepare", `{"confirm":true}`, true, false); w.Code != 403 {
		t.Fatal("CSRF not required")
	}
	if w := f.request("POST", path+"/prepare", `{}`, true, true); w.Code != 400 {
		t.Fatal("explicit confirmation not required")
	}
	if w := f.request("POST", path+"/prepare", `{"confirm":true}`, true, true); w.Code != 503 {
		t.Fatal("disabled IPC performed mutation")
	}
	if f.backend.calls != 0 {
		t.Fatal("disabled backend invoked")
	}
}
func TestSandboxHTTPAuthenticationLifecycleAndDeletionInterlock(t *testing.T) {
	f := newSandboxFixture(t, true)
	path := "/exits/" + f.exit.ID
	if w := f.request("GET", path+"/sandbox", "", false, false); w.Code != 401 {
		t.Fatal("anonymous sandbox query accepted")
	}
	if w := f.request("GET", "/executor", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"vpn_available":false`) {
		t.Fatal(w.Body.String())
	}
	if w := f.request("POST", path+"/sandbox/prepare", `{"confirm":true,"command":"id"}`, true, true); w.Code != 400 {
		t.Fatal("arbitrary command accepted")
	}
	if w := f.request("POST", "/exits/missing_exit/sandbox/prepare", `{"confirm":true}`, true, true); w.Code != 404 {
		t.Fatal("nonexistent exit accepted")
	}
	if f.backend.calls != 0 {
		t.Fatal("invalid requests invoked backend")
	}
	if w := f.request("POST", path+"/sandbox/prepare", `{"confirm":true}`, true, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"quarantine_verified":true`) {
		t.Fatal(w.Body.String())
	}
	if w := f.request("DELETE", path, "", true, true); w.Code != 409 {
		t.Fatal("deleted a configured exit with a live sandbox")
	}
	if w := f.request("POST", path+"/start", "", true, true); w.Code != 409 {
		t.Fatal("IPC falsely enabled VPN start")
	}
	overview := f.request("GET", "/overview", "", true, false)
	if !strings.Contains(overview.Body.String(), `"active_count":0`) || !strings.Contains(overview.Body.String(), `"protection_verified":false`) {
		t.Fatal("fabricated data plane state")
	}
	if w := f.request("POST", path+"/sandbox/cleanup", `{"confirm":true}`, true, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := f.request("DELETE", path, "", true, true); w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	events, err := f.s.Events()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
	}
	for _, kind := range []string{"sandbox.prepare_requested", "sandbox.prepare_completed", "sandbox.cleanup_requested", "sandbox.cleanup_completed"} {
		if !kinds[kind] {
			t.Fatalf("missing audit %s", kind)
		}
	}
}
func TestSandboxDeletionAndPrepareCannotRaceInControlPlane(t *testing.T) {
	f := newSandboxFixture(t, true)
	f.backend.entered = make(chan struct{})
	f.backend.release = make(chan struct{})
	path := "/exits/" + f.exit.ID
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.request("POST", path+"/sandbox/prepare", `{"confirm":true}`, true, true) }()
	select {
	case <-f.backend.entered:
	case <-time.After(time.Second):
		t.Fatal("prepare never began")
	}
	if w := f.request("DELETE", path, "", true, true); w.Code != 409 || !strings.Contains(w.Body.String(), "sandbox_busy") {
		t.Fatal("delete bypassed in-progress prepare", w.Body.String())
	}
	close(f.backend.release)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	f.backend.failure = errors.New("private external details")
	if w := f.request("DELETE", path, "", true, true); w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("unavailable daemon did not block deletion safely")
	}
	status := f.request("GET", "/executor", "", true, false)
	var data map[string]any
	if json.Unmarshal(status.Body.Bytes(), &data) != nil || data["reachable"] != false {
		t.Fatal("unavailable daemon reported reachable")
	}
}
