package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func dev36WebState(t *testing.T, root string, cfg WebSettings) {
	t.Helper()
	old, path := getWebSettings(), webSettingsPath
	base := currentBasePath()
	webSettingsMu.Lock()
	webSettingsCur = cfg
	webSettingsPath = filepath.Join(root, "settings.json")
	webSettingsMu.Unlock()
	basePathMu.Lock()
	basePathCur = ""
	basePathMu.Unlock()
	if err := saveWebSettings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		webSettingsMu.Lock()
		webSettingsCur = old
		webSettingsPath = path
		webSettingsMu.Unlock()
		basePathMu.Lock()
		basePathCur = base
		basePathMu.Unlock()
	})
}

func TestDev36DomainRoundTripKeepsListenerAndReturnsAddress(t *testing.T) {
	root := t.TempDir()
	cert, key := testCertificate(t, root, "node.example.com")
	port := freePort(t)
	cfg := WebSettings{Port: port, ListenAddr: "127.0.0.1", PublicIP: "127.0.0.1"}
	dev36WebState(t, root, cfg)
	server := newWebServer(nil)
	registry := &DomainRegistry{dir: root, server: server, values: []RegisteredDomain{{Domain: "node.example.com", CertFile: cert, KeyFile: key}}}
	server.handler = registry.Handler()
	if err := server.reload(cfg); err != nil {
		t.Fatal(err)
	}
	defer server.srv.Close()
	listener := server.ln
	pool := x509.NewCertPool()
	raw, _ := os.ReadFile(cert)
	pool.AppendCertsFromPEM(raw)
	transport := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: pool}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	request := func(scheme, host, body string) (int, map[string]any) {
		t.Helper()
		response, err := client.Post(scheme+"://"+net.JoinHostPort(host, strconv.Itoa(port))+"/api/domains", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value map[string]any
		if err = json.NewDecoder(response.Body).Decode(&value); err != nil {
			t.Fatalf("non-JSON response %d: %v", response.StatusCode, err)
		}
		return response.StatusCode, value
	}
	choice, err := certificateChoice(cert, key, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(domainRequest{Action: "bind", Domain: "node.example.com", CertificateID: choice.ID, Management: true})
	status, result := request("http", "127.0.0.1", string(body))
	if status != 200 || result["url"] != "https://node.example.com:"+strconv.Itoa(port)+"/" {
		t.Fatal(status, result)
	}
	if server.ln != listener {
		t.Fatal("binding reopened the TCP socket")
	}
	status, result = request("http", "127.0.0.1", `{"action":"unbind"}`)
	if status != 409 || result["code"] != "management_moved" {
		t.Fatal("old HTTP page did not receive new address", status, result)
	}
	if getWebSettings().TLSCert == "" {
		t.Fatal("old plaintext page changed HTTPS settings")
	}
	status, result = request("https", "node.example.com", `{"action":"unbind"}`)
	if status != 200 || result["url"] != "http://127.0.0.1:"+strconv.Itoa(port)+"/" {
		t.Fatal(status, result)
	}
	if server.ln != listener {
		t.Fatal("unbinding reopened the TCP socket")
	}
	status, result = request("https", "node.example.com", `{"action":"unbind"}`)
	if status != 409 || result["url"] != "http://127.0.0.1:"+strconv.Itoa(port)+"/" {
		t.Fatal("old HTTPS page lost the handoff", status, result)
	}
	status, _ = request("http", "127.0.0.1", `{"action":"unbind"}`)
	if status != 200 {
		t.Fatal("repeated unbind failed", status)
	}
}

func TestDev36SwitchingBehindProxyDoesNotDropConnections(t *testing.T) {
	cert, key := testCertificate(t, t.TempDir(), "node.example.com")
	cfg := WebSettings{Port: freePort(t), ListenAddr: "127.0.0.1", PublicIP: "127.0.0.1"}
	dev36WebState(t, t.TempDir(), cfg)
	server := newWebServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) }))
	if err := server.reload(cfg); err != nil {
		t.Fatal(err)
	}
	defer server.srv.Close()
	backend, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(cfg.Port))
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { http.Error(w, "backend unavailable", 503) }
	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	var wg sync.WaitGroup
	failures := make(chan string, 64)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for attempt := 0; attempt < 12; attempt++ {
				request, _ := http.NewRequest(http.MethodGet, proxyServer.URL+"/api/ping", nil)
				request.Header.Set("X-Forwarded-Proto", "https")
				response, err := client.Do(request)
				if err != nil {
					failures <- err.Error()
					continue
				}
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if response.StatusCode != 200 && response.StatusCode != 409 {
					failures <- response.Status + " " + string(body)
				}
				if response.StatusCode == 409 && !bytes.Contains(body, []byte("management_moved")) {
					failures <- string(body)
				}
			}
		}()
	}
	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			cfg.TLSCert, cfg.TLSKey = cert, key
		} else {
			cfg.TLSCert, cfg.TLSKey = "", ""
		}
		if err := server.reload(cfg); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error("request failed during switch:", failure)
	}
}

func TestDev36ProtocolHandoffDoesNotDisclosePrivateLoginPath(t *testing.T) {
	cfg := WebSettings{Port: 32001, PublicHost: "node.example.com", TLSCert: "fixture", TLSKey: "fixture"}
	dev36WebState(t, t.TempDir(), cfg)
	basePathMu.Lock()
	basePathCur = "/private-login"
	basePathMu.Unlock()
	server := &webServer{secure: true, settings: cfg}
	for _, path := range []string{"/", "/api/domains", "/private-login-other/api/domains"} {
		w := httptest.NewRecorder()
		server.handleWebRequest(w, httptest.NewRequest(http.MethodGet, "http://node.example.com"+path, nil))
		if w.Code != 404 || w.Header().Get("Location") != "" || w.Header().Get("X-Miku-Management-URL") != "" || strings.Contains(w.Body.String(), "private-login") {
			t.Fatal("private path disclosed", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	server.handleWebRequest(w, httptest.NewRequest(http.MethodPost, "http://node.example.com/private-login/api/domains", nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "https://node.example.com:32001/private-login/") {
		t.Fatal("known page did not receive handoff", w.Code, w.Body.String())
	}
}

func TestDev36IdleClientCannotBlockProtocolDetection(t *testing.T) {
	cfg := WebSettings{Port: freePort(t), ListenAddr: "127.0.0.1"}
	server := newWebServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ready") }))
	if err := server.reload(cfg); err != nil {
		t.Fatal(err)
	}
	defer server.srv.Close()
	idle, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(cfg.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
}

type temporaryWebAcceptError struct{}

func (temporaryWebAcceptError) Error() string   { return "fixture temporary accept failure" }
func (temporaryWebAcceptError) Temporary() bool { return true }
func (temporaryWebAcceptError) Timeout() bool   { return false }

type onceFailingWebListener struct {
	net.Listener
	failed bool
}

func (l *onceFailingWebListener) Accept() (net.Conn, error) {
	if !l.failed {
		l.failed = true
		return nil, temporaryWebAcceptError{}
	}
	return l.Listener.Accept()
}

func TestDev36TemporaryAcceptFailureDoesNotCloseManagementPort(t *testing.T) {
	base, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := newWebProtocolListener(&onceFailingWebListener{Listener: base}, &tls.Config{MinVersion: tls.VersionTLS12})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ready") })}
	go server.Serve(listener)
	defer server.Close()
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
}

func TestDev36HTTPSDomainReplacementHandsOffOldPage(t *testing.T) {
	root := t.TempDir()
	certA, keyA := testCertificate(t, root, "a.example.com")
	certB, keyB := testCertificate(t, root, "b.example.com")
	cfg := WebSettings{Port: freePort(t), ListenAddr: "127.0.0.1", PublicHost: "a.example.com", TLSCert: certA, TLSKey: keyA}
	dev36WebState(t, root, cfg)
	server := newWebServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) }))
	if err := server.reload(cfg); err != nil {
		t.Fatal(err)
	}
	defer server.srv.Close()
	next := cfg
	next.PublicHost = "b.example.com"
	next.TLSCert = certB
	next.TLSKey = keyB
	if err := server.applyWebSettings(next); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	for _, path := range []string{certA, certB} {
		raw, _ := os.ReadFile(path)
		pool.AppendCertsFromPEM(raw)
	}
	transport := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: pool}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for _, tc := range []struct {
		host   string
		status int
	}{{"a.example.com", 409}, {"b.example.com", 200}} {
		response, err := client.Get("https://" + net.JoinHostPort(tc.host, strconv.Itoa(cfg.Port)) + "/api/domains")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatal(tc.host, response.Status, string(raw))
		}
		if tc.status == 409 && !bytes.Contains(raw, []byte("https://b.example.com:"+strconv.Itoa(cfg.Port)+"/")) {
			t.Fatal("new domain missing", string(raw))
		}
	}
}

func TestDev36BusyNewPortKeepsOldSettingsAndListener(t *testing.T) {
	cfg := WebSettings{Port: freePort(t), ListenAddr: "127.0.0.1"}
	dev36WebState(t, t.TempDir(), cfg)
	server := newWebServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ready") }))
	if err := server.reload(cfg); err != nil {
		t.Fatal(err)
	}
	defer server.srv.Close()
	listener := server.ln
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	next := cfg
	next.Port = busy.Addr().(*net.TCPAddr).Port
	if server.applyWebSettings(next) == nil {
		t.Fatal("occupied port accepted")
	}
	if getWebSettings().Port != cfg.Port || server.ln != listener {
		t.Fatal("failed change replaced working configuration")
	}
	response, err := (&http.Client{Timeout: time.Second}).Get("http://127.0.0.1:" + strconv.Itoa(cfg.Port))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
}

func TestDev36EmailIsOptionalForCertificateIssuance(t *testing.T) {
	args, err := certificateCommandArgs(domainRequest{Domain: "node.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	joined := " " + strings.Join(args, " ") + " "
	if !strings.Contains(joined, " --register-unsafely-without-email ") || strings.Contains(joined, " --email ") || strings.Contains(joined, " -w ") {
		t.Fatal(args)
	}
	if _, err = certificateCommandArgs(domainRequest{Domain: "node.example.com", Email: "bad\n@example.com"}); err == nil {
		t.Fatal("unsafe optional email accepted")
	}
}

func TestDev36ForwardedHTTPSOnlyTrustedFromLocalProxy(t *testing.T) {
	for _, tc := range []struct {
		remote string
		want   bool
	}{{"127.0.0.1:1234", true}, {"[::1]:1234", true}, {"198.51.100.9:1234", false}} {
		r := httptest.NewRequest(http.MethodPost, "http://node.example.com/api/domains", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-Proto", "https")
		if got := requestHTTPS(r); got != tc.want {
			t.Fatal(tc.remote, got)
		}
	}
	auth, _, err := NewAuth(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://node.example.com/login", strings.NewReader(url.Values{"password": {auth.password}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	auth.Wrap(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS proxy login did not set a secure cookie", w.Code)
	}
	token, _ := auth.issue()
	r = httptest.NewRequest(http.MethodPost, "http://node.example.com/api/domains", nil)
	r.Header.Set("Origin", "https://node.example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "198.51.100.9:1234"
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w = httptest.NewRecorder()
	auth.Wrap(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("remote forwarded protocol bypassed Origin protection", w.Code)
	}
	// Existing HTTPS-offloading proxies can keep HTTP on their local backend.
	r = httptest.NewRequest(http.MethodPost, "http://node.example.com/api/domains", nil)
	r.Header.Set("Origin", "https://node.example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "127.0.0.1:1234"
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	server := &webServer{settings: WebSettings{Port: 32001, PublicHost: "127.0.0.1"}, handler: auth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) }))}
	w = httptest.NewRecorder()
	server.handleWebRequest(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("HTTPS-offloading proxy was rejected", w.Code, w.Body.String())
	}
}
