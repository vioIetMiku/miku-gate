package httpapi

import (
	"encoding/json"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngressInspectionRequiresAuthCSRFAndExplicitConsent(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	hash, _ := security.HashPassword("safe-test-password")
	if e = s.InitAccount("admin", hash); e != nil {
		t.Fatal(e)
	}
	calls := 0
	handler, e := NewWithOptions(s, nil, Options{InspectIngress: func(source ingress.Source) (ingress.Report, error) {
		calls++
		return ingress.Inspect(source, []byte(`{"inbounds":[{"tag":"test-vless","protocol":"vless","port":443,"settings":{"clients":[{"id":"secret-uuid"}]}}],"outbounds":[]}`))
	}})
	if e != nil {
		t.Fatal(e)
	}
	var cookie *http.Cookie
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:3939"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", token)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/api/host/ingress/sources", "", "", "").Code != 401 || request("POST", "/api/host/ingress/inspect", `{"source":"x-ui","confirm_read":true}`, "", "").Code != 401 {
		t.Fatal("unauthenticated config discovery")
	}
	login := request("POST", "/api/auth/login", `{"username":"admin","password":"safe-test-password"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie = login.Result().Cookies()[0]
	var session map[string]string
	json.Unmarshal(login.Body.Bytes(), &session)
	csrf := session["csrf"]
	if request("GET", "/api/host/ingress/sources", "", "", "").Code != 200 {
		t.Fatal("sources unavailable")
	}
	body := `{"source":"x-ui","confirm_read":true}`
	if request("POST", "/api/host/ingress/inspect", body, "", "").Code != 403 || request("POST", "/api/host/ingress/inspect", body, csrf, "https://evil.example").Code != 403 {
		t.Fatal("CSRF/origin bypass")
	}
	if request("POST", "/api/host/ingress/inspect", `{"source":"x-ui","confirm_read":false}`, csrf, "").Code != 400 || request("POST", "/api/host/ingress/inspect", `{"source":"/etc/shadow","confirm_read":true}`, csrf, "").Code != 400 || calls != 0 {
		t.Fatal("read before valid authorization")
	}
	response := request("POST", "/api/host/ingress/inspect", body, csrf, "")
	if response.Code != 200 || calls != 1 || strings.Contains(response.Body.String(), "secret-uuid") {
		t.Fatal("missing inspection or leaked client")
	}
	if request("POST", "/api/host/ingress/inspect", body, csrf, "").Code != 429 || calls != 1 {
		t.Fatal("missing inspection throttle")
	}
}
