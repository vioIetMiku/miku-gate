package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ipintel"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/vpngate"
)

func TestIntelligenceConsentAuthSourceAndActualExitBoundary(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := security.HashPassword("fixture")
	s.InitAccount("admin", hash)
	s.CreateSession("admin", "intel-session", "csrf", time.Now().Add(time.Hour))
	node := domain.Node{ID: "intel-node", IP: "8.8.8.8", Hostname: "vpn123", Source: vpngate.Source, FetchedAt: time.Now(), Config: "fixture never executed"}
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	exit, _, _ := s.CreateExit(domain.ExitInput{Name: "stopped", Country: "JP"})
	var calls atomic.Int32
	handler, err := NewWithOptions(s, nil, Options{LookupIntelligence: func(_ context.Context, ip string) (ipintel.Report, error) {
		calls.Add(1)
		return ipintel.Report{IP: ip, Residential: "unknown", NetworkPath: ipintel.PathDescription}, nil
	}, ResolveIntelligenceDomain: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, body string, auth, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.AddCookie(&http.Cookie{Name: "miku_session", Value: "intel-session"})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/nodes/" + node.ID + "/intelligence"
	if request(path, `{"confirm":true}`, false, false).Code != 401 || request(path, `{"confirm":true}`, true, false).Code != 403 || request(path, `{"confirm":false}`, true, true).Code != 400 || request(path, `{"confirm":true,"ip":"127.0.0.1"}`, true, true).Code != 400 || calls.Load() != 0 {
		t.Fatal("unconsented/unauthorized provider query")
	}
	if request("/api/exits/"+exit.ID+"/intelligence", `{"confirm":true}`, true, true).Code != 409 || calls.Load() != 0 {
		t.Fatal("stopped candidate used as measured exit")
	}
	if request("/api/intelligence/domain", `{"hostname":"entry.example","confirm":true}`, true, true).Code != 400 || calls.Load() != 0 {
		t.Fatal("private DNS recipient queried")
	}
	// Fresh handler resets query limiter after the DNS negative test.
	handler, err = NewWithOptions(s, nil, Options{LookupIntelligence: func(_ context.Context, ip string) (ipintel.Report, error) {
		calls.Add(1)
		return ipintel.Report{IP: ip, Residential: "unknown"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(path, `{"confirm":true}`, true, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"residential":"unknown"`) || calls.Load() != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	if request(path, `{"confirm":true}`, true, true).Code != 429 || calls.Load() != 1 {
		t.Fatal("metadata query rate cap bypass")
	}
	node.Source = "untrusted"
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	if request(path, `{"confirm":true}`, true, true).Code != 409 || calls.Load() != 1 {
		t.Fatal("untrusted source queried")
	}
}
