package httpapi

import (
	"crypto/tls"
	"encoding/json"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicTLSExactHostOriginSecureCookieAndCSRF(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	hash, _ := security.HashPassword("strong-test-pass")
	s.InitAccount("admin", hash)
	h, e := NewWithOptions(s, nil, Options{PublicOrigin: "https://entry.example:8443"})
	if e != nil {
		t.Fatal(e)
	}
	var cookie *http.Cookie
	call := func(method, path, body, host, origin, token string, secured bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://entry.example:8443"+path, strings.NewReader(body))
		r.Host = host
		r.TLS = nil
		if secured {
			r.TLS = &tls.ConnectionState{}
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", token)
		r.Header.Set("X-Forwarded-Proto", "https")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := `{"username":"admin","password":"strong-test-pass"}`
	if call("POST", "/api/auth/login", login, "entry.example:8443", "https://entry.example:8443", "", false).Code != 403 {
		t.Fatal("forwarded plaintext")
	}
	for _, origin := range []string{"http://entry.example:8443", "https://entry.example:8443/", "https://evil.example", "null"} {
		if call("POST", "/api/auth/login", login, "entry.example:8443", origin, "", true).Code != 403 {
			t.Fatal("Origin")
		}
	}
	for _, host := range []string{"localhost:8443", "entry.example", "evil.example:8443"} {
		if call("GET", "/api/health", "", host, "", "", true).Code != 403 {
			t.Fatal("Host alias")
		}
	}
	w := call("POST", "/api/auth/login", login, "entry.example:8443", "https://entry.example:8443", "", true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie = w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie")
	}
	var session map[string]string
	json.Unmarshal(w.Body.Bytes(), &session)
	if call("GET", "/api/exits", "", "entry.example:8443", "", "", true).Code != 200 {
		t.Fatal("session")
	}
	if call("POST", "/api/exits", `{"name":"test","country":"JP"}`, "entry.example:8443", "https://entry.example:8443", "", true).Code != 403 {
		t.Fatal("CSRF")
	}
	w = call("POST", "/api/auth/logout", "{}", "entry.example:8443", "https://entry.example:8443", session["csrf"], true)
	if w.Code != 204 || !w.Result().Cookies()[0].Secure {
		t.Fatal("logout cookie")
	}
	if call("GET", "/api/exits", "", "entry.example:8443", "", "", true).Code != 401 {
		t.Fatal("revocation")
	}
}
func TestInvalidPublicOriginFailsConstruction(t *testing.T) {
	if _, e := NewWithOptions(nil, nil, Options{PublicOrigin: "http://entry.example:8443"}); e == nil {
		t.Fatal("plaintext allowed")
	}
}
