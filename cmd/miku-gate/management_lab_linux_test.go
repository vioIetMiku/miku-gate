//go:build linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"mikugate/internal/management"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLinuxManagementHTTPSCLI(t *testing.T) {
	if os.Getenv("MIKU_GATE_MANAGEMENT_TEST") != "1" {
		t.Skip("explicit real CLI HTTPS lab required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("control CLI must ordinary uid")
	}
	binary := os.Getenv("MIKU_GATE_BINARY")
	if !filepath.IsAbs(binary) {
		t.Fatal("exact CLI executable required")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	certdir := filepath.Join(dir, "certificate")
	identity, e := management.BootstrapIP("192.0.2.10", certdir)
	if e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(dir, "data")
	init := exec.Command(binary, "init", "--data", data)
	raw, e := init.Output()
	if e != nil {
		t.Fatal("init")
	}
	matches := regexp.MustCompile(`密码：([^\r\n]+)`).FindSubmatch(raw)
	if len(matches) != 2 {
		t.Fatal("one-time identity")
	}
	password := string(matches[1])
	reserve, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := reserve.Addr().String()
	reserve.Close()
	_, port, _ := net.SplitHostPort(address)
	origin := "https://192.0.2.10:" + port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve", "--data", data, "--listen", "127.0.0.1:"+port, "--public-origin", origin, "--tls-certificate", filepath.Join(certdir, "certificate.pem"), "--tls-key", filepath.Join(certdir, "private.key"), "--allow-bootstrap-certificate", "--control-plane-only")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		t.Fatal("start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cancel(); <-done })
	cert, _ := os.ReadFile(filepath.Join(certdir, "certificate.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "192.0.2.10", MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", "127.0.0.1:"+port)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(method, path, body, host, requestOrigin, cookie, csrf string) (int, []byte, []*http.Cookie) {
		r, _ := http.NewRequest(method, origin+path, strings.NewReader(body))
		r.Host = host
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if requestOrigin != "" {
			r.Header.Set("Origin", requestOrigin)
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(r)
		if e != nil {
			return 0, nil, nil
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 65536))
		return response.StatusCode, raw, response.Cookies()
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		code, _, _ := request("GET", "/api/health", "", "192.0.2.10:"+port, "", "", "")
		if code == 200 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
	code, body, cookies := request("POST", "/api/auth/login", string(login), "192.0.2.10:"+port, origin, "", "")
	if code != 200 || len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("real IP HTTPS login failed")
	}
	var session map[string]any
	json.Unmarshal(body, &session)
	cookie := cookies[0].Name + "=" + cookies[0].Value
	if code, _, _ := request("GET", "/api/exits", "", "evil.example:"+port, "", cookie, ""); code != 403 {
		t.Fatal("Host mismatch accepted")
	}
	if code, _, _ := request("POST", "/api/auth/logout", "{}", "192.0.2.10:"+port, "http://192.0.2.10:"+port, cookie, session["csrf"].(string)); code != 403 {
		t.Fatal("wrong scheme origin")
	}
	if code, _, _ := request("POST", "/api/auth/logout", "{}", "192.0.2.10:"+port, origin, cookie, session["csrf"].(string)); code != 204 {
		t.Fatal("logout")
	}
	if code, _, _ := request("GET", "/api/exits", "", "192.0.2.10:"+port, "", cookie, ""); code != 401 {
		t.Fatal("revocation")
	}
	if identity.SystemTrusted {
		t.Fatal("private IP bootstrap called system-trusted")
	}
	t.Log("Observed real ordinary UID CLI, IP SAN HTTPS verified by explicit certificate trust without TLS skip, exact Host/Origin, Secure session and logout/revocation. Not external public address reachability.")
}
