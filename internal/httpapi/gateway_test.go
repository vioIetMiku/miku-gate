package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/security"
	"mikugate/internal/store"
)

type apiSession struct {
	target string
	closed atomic.Int32
}

func (s *apiSession) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp4", s.target)
}
func (s *apiSession) Probe(context.Context) (gateway.Proof, error) {
	return gateway.Proof{Kind: "https", ProtectionMode: "proxy_only", DNSVerified: true}, nil
}
func (s *apiSession) Close() error { s.closed.Add(1); return nil }

// Outbound protocol/TLS/DoH is exercised in upstream's real-socket suite. This
// integration tests the actual HTTP API, encrypted store, runtime, auth listener
// and mutation locks with a controlled dial-session factory (no public network).
func TestConfiguredRuntimeAPIAndActualProxyTraffic(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := security.HashPassword("admin-password")
	if err = s.InitAccount("admin", hash); err != nil {
		t.Fatal(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached target")
		}
		io.WriteString(w, "runtime-api-route")
	}))
	defer target.Close()
	session := &apiSession{target: target.Listener.Addr().String()}
	manager, err := gateway.New(t.Context(), s, gateway.Options{HealthInterval: time.Hour, Factory: func(_ context.Context, _ domain.Exit, c *domain.UpstreamConfig) (gateway.Session, error) {
		if c == nil || c.Password != "hidden-upstream-password" {
			t.Error("encrypted configuration did not reach factory")
		}
		return session, nil
	}, Listen: func(ctx context.Context, _, _ string) (net.Listener, error) {
		return (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	handler, err := NewWithOptions(s, nil, Options{Gateway: manager})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	var cookie *http.Cookie
	csrf := ""
	request := func(method, path string, body any, withCSRF bool) (int, []byte) {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, _ = json.Marshal(body)
		}
		req, err := http.NewRequest(method, httpServer.URL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if withCSRF {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if path == "/api/auth/login" && response.StatusCode == 200 {
			cookie = response.Cookies()[0]
			var data map[string]any
			json.Unmarshal(raw, &data)
			csrf = data["csrf"].(string)
		}
		return response.StatusCode, raw
	}
	if status, _ := request("GET", "/api/exits/missing/upstream", nil, false); status != 401 {
		t.Fatal("unprotected upstream API", status)
	}
	if status, raw := request("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "admin-password"}, false); status != 200 {
		t.Fatal(status, string(raw))
	}
	status, raw := request("POST", "/api/exits", domain.ExitInput{Name: "API runtime", Country: "JP"}, true)
	if status != 201 {
		t.Fatal(status, string(raw))
	}
	var created struct {
		Exit     domain.Exit `json:"exit"`
		Password string      `json:"password"`
	}
	if err = json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	prefix := "/api/exits/" + created.Exit.ID
	config := domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443, Username: "up-user", Password: "hidden-upstream-password"}
	if status, _ = request("PUT", prefix+"/upstream", config, false); status != 403 {
		t.Fatal("missing CSRF accepted", status)
	}
	if status, raw = request("PUT", prefix+"/upstream", config, true); status != 204 {
		t.Fatal(status, string(raw))
	}
	if status, raw = request("GET", prefix+"/upstream", nil, false); status != 200 || bytes.Contains(raw, []byte(config.Password)) {
		t.Fatal("secret view leak", status, string(raw))
	}
	var view domain.UpstreamView
	json.Unmarshal(raw, &view)
	if !view.Configured || !view.HasPassword {
		t.Fatal("missing protected configuration flags")
	}
	status, raw = request("POST", prefix+"/start", nil, true)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var state gateway.State
	json.Unmarshal(raw, &state)
	if !state.ProxyListening || !state.Healthy || state.Proof.KernelVerified {
		t.Fatal("false runtime evidence", state)
	}
	status, raw = request("GET", "/api/overview", nil, false)
	var overview map[string]any
	json.Unmarshal(raw, &overview)
	if status != 200 || overview["active_count"] != float64(1) || overview["proxy_runtime_available"] != true || overview["executor_available"] != false {
		t.Fatal("wrong actual counts", string(raw))
	}
	client, err := net.Dial("tcp4", state.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	auth := base64.StdEncoding.EncodeToString([]byte(created.Exit.Username + ":" + created.Password))
	io.WriteString(client, "GET http://business.example/ HTTP/1.1\r\nHost: business.example\r\nProxy-Authorization: Basic "+auth+"\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != "runtime-api-route" {
		t.Fatal("actual authenticated proxy failed", err)
	}
	for _, operation := range []struct {
		method, path string
		body         any
	}{{"PATCH", prefix, domain.ExitInput{Name: "changed", Country: "KR"}}, {"DELETE", prefix, nil}, {"POST", prefix + "/credentials", nil}, {"PUT", prefix + "/upstream", config}, {"DELETE", prefix + "/upstream", nil}} {
		if status, raw = request(operation.method, operation.path, operation.body, true); status != 409 {
			t.Fatalf("active mutation accepted %s %s: %d %s", operation.method, operation.path, status, raw)
		}
	}
	if status, raw = request("POST", prefix+"/probe", nil, true); status != 200 {
		t.Fatal(status, string(raw))
	}
	if status, raw = request("POST", prefix+"/stop", nil, true); status != 204 {
		t.Fatal(status, string(raw))
	}
	if session.closed.Load() != 1 {
		t.Fatal("runtime session not closed once")
	}
	if status, raw = request("GET", "/api/overview", nil, false); status != 200 || !strings.Contains(string(raw), `"active_count":0`) {
		t.Fatal("stale active count", string(raw))
	}
	if status, raw = request("POST", prefix+"/credentials", nil, true); status != 200 {
		t.Fatal("stopped credential rotation failed", status, string(raw))
	}
	if status, _ = request("DELETE", prefix+"/upstream", nil, true); status != 204 {
		t.Fatal(status)
	}
	if status, _ = request("DELETE", prefix, nil, true); status != 204 {
		t.Fatal(status)
	}
}
