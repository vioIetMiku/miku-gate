package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/vpngate"
)

func TestIdentityAPIRequiresAuthCSRFAndServerCachedObservation(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := security.HashPassword("test-only")
	s.InitAccount("admin", hash)
	s.CreateSession("admin", "identity-session", "csrf", time.Now().Add(time.Hour))
	node := domain.Node{ID: "tls-identity-test", Hostname: "vpn123", IP: "8.8.8.8", Source: vpngate.Source, FetchedAt: time.Now(), Config: "synthetic never executed"}
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	var calls atomic.Int32
	inspect := func(_ context.Context, n domain.Node) (domain.NodeIdentity, error) {
		calls.Add(1)
		return domain.NodeIdentity{NodeID: n.ID, IP: n.IP, TLSHost: "vpn123.opengw.net", TLSPort: 443, PeerName: "opengw.net", PeerFingerprint: strings.Repeat("a", 64), ProfileSHA256: vpngate.ProfileDigest(n.Config), CheckedAt: time.Now().UTC(), NotAfter: time.Now().Add(time.Hour), Source: vpngate.Source, Verified: true}, nil
	}
	handler, err := NewWithOptions(s, nil, Options{InspectNodeIdentity: inspect})
	if err != nil {
		t.Fatal(err)
	}
	request := func(auth, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost/api/nodes/"+node.ID+"/identity", nil)
		if auth {
			r.AddCookie(&http.Cookie{Name: "miku_session", Value: "identity-session"})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request(false, false).Code != 401 || request(true, false).Code != 403 || calls.Load() != 0 {
		t.Fatal("unauthorized identity probe contacted network")
	}
	w := request(true, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got domain.NodeIdentity
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || !got.Verified || got.VPNConnected || got.PeerFingerprint == "" {
		t.Fatal("bad identity result")
	}
	if request(true, true).Code != 200 || calls.Load() != 1 {
		t.Fatal("valid cached identity made duplicate network requests")
	}
	node.Config = "changed source"
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	if request(true, true).Code != 429 || calls.Load() != 1 {
		t.Fatal("changed profile reused old identity or bypassed rate cap")
	}
}
