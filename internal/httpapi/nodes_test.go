package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/tunnel"
	"mikugate/internal/vpngate"
)

func TestNodeProfileCheckIsAuthenticatedReadOnlyAndRedacted(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := security.HashPassword("test-only")
	if err = s.InitAccount("admin", hash); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateSession("admin", "node-inspection-session", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	secretMarker := "private-material-must-not-appear"
	nodes := []domain.Node{{ID: "official-one", Hostname: "vpn1", IP: "8.8.8.8", Country: "JP", Source: vpngate.Source, FetchedAt: time.Now(), Config: "client\ndev tun\nremote 8.8.8.8 443\n<key>\n" + secretMarker + "\n</key>\n"}, {ID: "unexpected-source", Hostname: "other", IP: "1.1.1.1", Source: "https://example.invalid/", Config: "do not execute"}}
	if err = s.ReplaceNodes(t.Context(), nodes); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Events()
	handler, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(id string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://localhost/api/nodes/"+id+"/profile", nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "miku_session", Value: "node-inspection-session"})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("official-one", false); w.Code != 401 {
		t.Fatal("anonymous profile inspection accepted")
	}
	w := call("official-one", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), secretMarker) || strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatal("profile material leaked", w.Code)
	}
	var check tunnel.ProfileCheck
	if err = json.Unmarshal(w.Body.Bytes(), &check); err != nil || check.Mode != "unsupported" || check.NetworkTested || check.ServerIdentityVerified {
		t.Fatal("format inspection claimed connection or identity verification", err)
	}
	if w = call("unexpected-source", true); w.Code != 409 {
		t.Fatal("unexpected source accepted")
	}
	if w = call("missing", true); w.Code != 404 {
		t.Fatal("missing node accepted")
	}
	after, _ := s.Events()
	if len(before) != len(after) {
		t.Fatal("read-only check changed audit/state")
	}
}
