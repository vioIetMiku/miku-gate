package httpapi

import (
	"encoding/json"
	"mikugate/internal/certassets"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedPreviewAndCertificateNeedAuthCSRFConsent(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	hash, _ := security.HashPassword("safe-test-password")
	s.InitAccount("admin", hash)
	calls := 0
	h, e := NewWithOptions(s, nil, Options{DiscoverCertificate: func(host string) (certassets.Discovery, *certassets.Asset, error) {
		calls++
		return certassets.Discovery{Hostname: host, Findings: []certassets.Finding{{Provider: "test", State: "not_found"}}}, nil, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	var cookie *http.Cookie
	req := func(method, path, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:3939"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if req("GET", "/api/ingress/templates", "", "").Code != 401 || req("GET", "/api/ingress/reality/candidates", "", "").Code != 401 {
		t.Fatal("unprotected managed metadata")
	}
	login := req("POST", "/api/auth/login", `{"username":"admin","password":"safe-test-password"}`, "")
	cookie = login.Result().Cookies()[0]
	var session map[string]string
	json.Unmarshal(login.Body.Bytes(), &session)
	csrf := session["csrf"]
	if req("GET", "/api/ingress/templates", "", "").Code != 200 || req("GET", "/api/ingress/reality/candidates", "", "").Code != 200 {
		t.Fatal("missing choices")
	}
	body := `{"core":"xray","template":"vless-tls-vision","address":"entry.example","port":443,"server_name":"entry.example"}`
	if req("POST", "/api/ingress/preview", body, "").Code != 403 {
		t.Fatal("CSRF preview missing")
	}
	w := req("POST", "/api/ingress/preview", body, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":false`) {
		t.Fatal("preview wrong")
	}
	if req("POST", "/api/ingress/certificate", `{"hostname":"entry.example","confirm_read":false}`, csrf).Code != 400 || calls != 0 {
		t.Fatal("read without consent")
	}
	if req("POST", "/api/ingress/reality/probe", `{"hostname":"127.0.0.1","confirm_network":true}`, csrf).Code != 400 {
		t.Fatal("private target accepted")
	}
	w = req("POST", "/api/ingress/certificate", `{"hostname":"entry.example","confirm_read":true}`, csrf)
	if w.Code != 200 || calls != 1 || strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatal("unsafe certificate output")
	}
	if req("POST", "/api/ingress/certificate", `{"hostname":"entry.example","confirm_read":true}`, csrf).Code != 429 || calls != 1 {
		t.Fatal("missing throttle")
	}
}
