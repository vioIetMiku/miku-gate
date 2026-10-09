package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mikugate/internal/domain"
	"mikugate/internal/security"
	"mikugate/internal/store"
)

func TestAuthenticatedLifecycle(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := security.HashPassword("safe-password")
	if err = s.InitAccount("admin", hash); err != nil {
		t.Fatal(err)
	}
	handler, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	csrf := ""
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:3939"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if token != "" {
			r.Header.Set("X-CSRF-Token", token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/exits", "", "", ""); w.Code != 401 {
		t.Fatalf("unprotected exits: %d", w.Code)
	}
	w := request("POST", "/api/auth/login", `{"username":"admin","password":"safe-password"}`, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie = w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	var login map[string]string
	json.Unmarshal(w.Body.Bytes(), &login)
	csrf = login["csrf"]
	body := `{"name":"日本开发","country":"JP","policy":"any","port":20001}`
	if w = request("POST", "/api/exits", body, "", ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w = request("POST", "/api/exits", body, csrf, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	if w = request("POST", "/api/exits", `{"name":"bad","country":"JP","unexpected":1}`, csrf, ""); w.Code != 400 {
		t.Fatal("unknown field accepted")
	}
	w = request("POST", "/api/exits", body, csrf, "")
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		Exit     domain.Exit `json:"exit"`
		Password string      `json:"password"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.Password == "" {
		t.Fatal("missing one-time password")
	}
	w = request("GET", "/api/exits", "", "", "")
	if strings.Contains(w.Body.String(), created.Password) || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("secret leak")
	}
	if w = request("POST", "/api/exits/"+created.Exit.ID+"/start", "", csrf, ""); w.Code != 409 {
		t.Fatal("fake start succeeded")
	}
	w = request("GET", "/api/overview", "", "", "")
	if !strings.Contains(w.Body.String(), `"active_count":0`) || !strings.Contains(w.Body.String(), `"protection_verified":false`) {
		t.Fatal("fabricated runtime status")
	}
	w = request("POST", "/api/auth/logout", "", csrf, "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	if w = request("GET", "/api/exits", "", "", ""); w.Code != 401 {
		t.Fatal("revoked session usable")
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/health", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
}
func TestLoginRateLimit(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	handler, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "http://localhost/api/auth/login", strings.NewReader(`{"username":"missing","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		expected := 401
		if i == 5 {
			expected = 429
		}
		if w.Code != expected {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
}

func TestJSONContentType(t *testing.T) {
	for _, typ := range []string{"application/jsonx", "text/plain", "application/json; broken"} {
		r := httptest.NewRequest("POST", "http://localhost/", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", typ)
		w := httptest.NewRecorder()
		var value map[string]any
		if decode(w, r, &value) || w.Code != 415 {
			t.Fatalf("accepted invalid media type %q", typ)
		}
	}
	r := httptest.NewRequest("POST", "http://localhost/", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	var value map[string]any
	if !decode(w, r, &value) {
		t.Fatal("valid media type rejected")
	}
}
