package httpapi

import (
	"encoding/json"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedRuntimeAPIConsentPersistenceAndClientSecretBoundary(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	hash, _ := security.HashPassword("safe-test-password")
	s.InitAccount("admin", hash)
	exit, password, _ := s.CreateExit(domain.ExitInput{Name: "managed", Country: "JP", Port: 22000})
	h, e := New(s, nil)
	if e != nil {
		t.Fatal(e)
	}
	var cookie *http.Cookie
	req := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:3939"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	prefix := "/api/exits/" + exit.ID + "/ingress"
	for _, path := range []string{prefix, prefix + "/runtime"} {
		if req("GET", path, "", "").Code != 401 {
			t.Fatal("anonymous secret/config API")
		}
	}
	login := req("POST", "/api/auth/login", `{"username":"admin","password":"safe-test-password"}`, "")
	cookie = login.Result().Cookies()[0]
	var session map[string]string
	json.Unmarshal(login.Body.Bytes(), &session)
	csrf := session["csrf"]
	if req("POST", prefix+"/start", `{"confirm":true}`, "").Code != 403 {
		t.Fatal("missing core CSRF")
	}
	if req("POST", prefix+"/start", `{"confirm":true}`, csrf).Code != 503 {
		t.Fatal("implicit core run")
	}
	if req("POST", prefix+"/client", `{"confirm":false}`, csrf).Code != 400 {
		t.Fatal("client secret reveal without consent")
	}
	_, hash, _ = s.ExitForRuntime(exit.ID)
	m := ingress.ManagedMaterial{Request: ingress.ManagedRequest{Core: "sing-box", Template: "vless-reality-vision", Address: "entry.example", Listen: "127.0.0.1", Port: 25009, ServerName: "target.example", RealityHostname: "target.example"}, Proxy: ingress.Proxy{Port: 22000, Username: exit.Username, Password: password}, Identity: ingress.ManagedIdentity{UUID: "7c664910-2503-4f16-853f-636c5c68ff90", Password: strings.Repeat("a", 32), RealityPrivateKey: "aRlwIfRYFL8CDWSaxjwjorGXIZAzTy9hUXGgBFfltmI", ShortID: "071c5e3d4b09ab5e"}, Target: &ingress.RealityTarget{Hostname: "target.example", Address: "8.8.8.8", Port: 443, CheckedAt: time.Now(), NotAfter: time.Now().Add(24 * time.Hour), TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true}}
	if e = s.SaveManaged(exit.ID, m, hash); e != nil {
		t.Fatal(e)
	}
	view := req("GET", prefix, "", "")
	if view.Code != 200 || strings.Contains(view.Body.String(), password) || strings.Contains(view.Body.String(), m.Identity.UUID) || strings.Contains(view.Body.String(), m.Identity.RealityPrivateKey) {
		t.Fatal("view exposed recoverable secrets")
	}
	state := req("GET", prefix+"/runtime", "", "")
	if !strings.Contains(state.Body.String(), `"phase":"stopped"`) {
		t.Fatal("database inferred runtime")
	}
	reveal := req("POST", prefix+"/client", `{"confirm":true}`, csrf)
	if reveal.Code != 200 || !strings.Contains(reveal.Body.String(), m.Identity.UUID) || strings.Contains(reveal.Body.String(), password) || strings.Contains(reveal.Body.String(), m.Identity.RealityPrivateKey) {
		t.Fatal("client output missing credential or leaked server/exit key")
	}
	s.DeleteManaged(exit.ID)
	// API unavailable paths do not accept browser-supplied certificate/probe flags
	// to forge server material. No production network is used by this API test.
	body := `{"request":{"core":"sing-box","template":"vless-reality-vision","address":"entry.example","listen":"127.0.0.1","port":25009,"server_name":"target.example","reality_hostname":"target.example"},"exit_password":"wrong","confirm":true,"confirm_network":true}`
	if req("PUT", prefix, body, csrf).Code != 403 {
		t.Fatal("wrong proxy password accepted")
	}
	if req("DELETE", prefix, "", csrf).Code != 204 {
		t.Fatal("remove")
	}
}
