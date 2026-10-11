//go:build linux

package paneladapter

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mikugate/internal/ingress"
	"mikugate/internal/proxy"
	"mikugate/internal/security"
)

type officialProcess struct {
	command *exec.Cmd
	done    chan error
}

func startOfficial(t *testing.T, binary, dir string, env []string, args ...string) *officialProcess {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = env
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if command.Start() != nil {
		t.Fatal("official child start failed; private diagnostics omitted")
	}
	p := &officialProcess{command: command, done: make(chan error, 1)}
	go func() { p.done <- command.Wait() }()
	return p
}
func (p *officialProcess) stop() {
	if p == nil {
		return
	}
	_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}
func labPort(t *testing.T) int {
	t.Helper()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}
func labWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil || os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("private lab JSON write")
	}
}
func labCA(t *testing.T, root string) (string, string, string) {
	t.Helper()
	now := time.Now()
	caKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal("CA generation")
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Miku real panel private lab root"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(72 * time.Hour)}
	caRaw, e := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal("CA signing")
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"panel-ingress.test"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
	raw, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal("leaf signing")
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	cert, k, cap := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem"), filepath.Join(root, "ca.pem")
	for path, data := range map[string][]byte{cert: append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})...), k: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), cap: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})} {
		if os.WriteFile(path, data, 0600) != nil {
			t.Fatal("private certificate write")
		}
	}
	return cert, k, cap
}
func panelLoginEventually(t *testing.T, endpoint Endpoint, user, password string) *Client {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		value, e := New(endpoint)
		if e == nil {
			client := value.(*Client)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			e = client.Login(ctx, user, password, "")
			cancel()
			if e == nil {
				return client
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("official panel startup/login/CSRF unavailable; no password/raw log returned")
	return nil
}
func labPanelAction(t *testing.T, client *Client, path string, form url.Values) json.RawMessage {
	t.Helper()
	data := form.Encode()
	request, e := http.NewRequestWithContext(t.Context(), "POST", client.base+path, strings.NewReader(data))
	if e != nil {
		t.Fatal("private API action")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", client.origin)
	request.Header.Set("Referer", client.base)
	request.Header.Set("X-CSRF-Token", client.csrf)
	response, e := client.http.Do(request)
	if e != nil {
		t.Fatal("official inbound API unreachable")
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	var safe struct {
		Success bool            `json:"success"`
		Object  json.RawMessage `json:"obj"`
	}
	if e != nil || len(raw) > 1<<20 || response.StatusCode != 200 || json.Unmarshal(raw, &safe) != nil || !safe.Success {
		t.Fatal("official inbound mutation refused; private output omitted")
	}
	return safe.Object
}
func waitPanelRoute(t *testing.T, c *Client, before Snapshot, plan ingress.Plan) Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, e := c.Read(t.Context())
		if e == nil && Verify(before, snapshot, plan) == nil {
			return snapshot
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("official panel generated route/identity not verified after persistent save")
	return Snapshot{}
}
func labEcho(clientPort, targetPort int, host string) bool {
	connection, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(clientPort)), time.Second)
	if e != nil {
		return false
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	connection.Write([]byte{5, 1, 0})
	var method [2]byte
	if _, e = io.ReadFull(connection, method[:]); e != nil || method != [2]byte{5, 0} {
		return false
	}
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	request = append(request, byte(targetPort>>8), byte(targetPort))
	connection.Write(request)
	var reply [4]byte
	if _, e = io.ReadFull(connection, reply[:]); e != nil || reply[1] != 0 {
		return false
	}
	size := 0
	switch reply[3] {
	case 1:
		size = 6
	case 4:
		size = 18
	case 3:
		var length [1]byte
		if _, e = io.ReadFull(connection, length[:]); e != nil {
			return false
		}
		size = int(length[0]) + 2
	default:
		return false
	}
	if _, e = io.CopyN(io.Discard, connection, int64(size)); e != nil {
		return false
	}
	payload := []byte("official-panel-encrypted-persistent-proof")
	connection.Write(payload)
	echo := make([]byte, len(payload))
	_, e = io.ReadFull(connection, echo)
	return e == nil && bytes.Equal(payload, echo)
}
func waitEcho(t *testing.T, port, target int, host string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if labEcho(port, target, host) {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
	t.Fatal("actual TLS Vision client business path failed; no raw private diagnostic returned")
}

func TestLinuxOfficial3XUIPersistentRoute(t *testing.T) {
	if os.Getenv("MIKU_GATE_OFFICIAL_PANEL_LAB") != "1" {
		t.Skip("explicit official panel loopback-only lab")
	}
	if os.Geteuid() == 0 {
		t.Fatal("official panel and cores must ordinary UID")
	}
	links, e := net.Interfaces()
	if e != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("anonymous loopback-only network namespace required")
	}
	panelBinary, assetDir, xray := os.Getenv("MIKU_GATE_PANEL_BINARY"), os.Getenv("MIKU_GATE_PANEL_ASSETS"), os.Getenv("MIKU_GATE_XRAY")
	for _, path := range []string{panelBinary, assetDir, xray} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact staged official paths required")
		}
	}
	root := t.TempDir()
	os.Chmod(root, 0700)
	bin, db, logs := filepath.Join(root, "bin"), filepath.Join(root, "db"), filepath.Join(root, "logs")
	for _, path := range []string{bin, db, logs} {
		if os.Mkdir(path, 0700) != nil {
			t.Fatal("lab directories")
		}
	}
	entries, e := os.ReadDir(assetDir)
	if e != nil {
		t.Fatal("official asset staging")
	}
	for _, item := range entries {
		if item.IsDir() {
			continue
		}
		name := item.Name()
		if name == "geoip.dat" || name == "geosite.dat" {
			if os.Symlink(filepath.Join(assetDir, name), filepath.Join(bin, name)) != nil {
				t.Fatal("official read-only geodata link")
			}
		}
	}
	if os.Symlink(xray, filepath.Join(bin, "xray-linux-amd64")) != nil {
		t.Fatal("official panel embedded core path")
	}
	panelPort, inboundPort, otherPort, clientPort := labPort(t), labPort(t), labPort(t), labPort(t)
	password, _ := security.Token(18)
	env := append(os.Environ(), "XUI_DB_FOLDER="+db, "XUI_BIN_FOLDER="+bin, "XUI_LOG_FOLDER="+logs, "XUI_LOG_LEVEL=error", "XUI_PORT="+strconv.Itoa(panelPort), "GOMAXPROCS=1", "GOMEMLIMIT=160MiB")
	settings := exec.Command(panelBinary, "setting", "-port", strconv.Itoa(panelPort), "-username", "miku-panel-lab", "-password", password, "-webBasePath", "/miku-lab/", "-listenIP", "127.0.0.1")
	settings.Env = env
	settings.Dir = root
	settings.Stdout = io.Discard
	settings.Stderr = io.Discard
	if settings.Run() != nil {
		t.Fatal("official panel isolated initialization failed")
	}
	panel := startOfficial(t, panelBinary, root, env)
	t.Cleanup(func() {
		if panel != nil {
			panel.stop()
		}
	})
	endpoint := Endpoint{Port: panelPort, Base: "/miku-lab/", Mode: "current"}
	client := panelLoginEventually(t, endpoint, "miku-panel-lab", password)
	t.Log("Official pinned 3x-ui ordinary process login and CSRF handshake passed; no host DB/services accessed.")
	before, e := client.Read(t.Context())
	if e != nil {
		t.Fatal("official persistent settings/generated JSON read incompatible")
	}
	// Keep actual original template for restore; remove only the isolated fresh
	// panel's external geo/DNS defaults before adding private-loopback fixture.
	minimal, _ := Canonical([]byte(`{"log":{"loglevel":"error"},"inbounds":[],"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"blocked","protocol":"blackhole"}],"routing":{"domainStrategy":"AsIs","rules":[]}}`))
	if client.Save(t.Context(), Snapshot{Template: minimal, TestURL: before.TestURL}) != nil {
		t.Fatal("official initial minimal isolated template save")
	}
	cert, key, ca := labCA(t, root)
	uuid := "7c664910-2503-4f16-853f-636c5c68ff90"
	create := func(port int, name string) {
		settings, _ := json.Marshal(map[string]any{"clients": []any{map[string]any{"id": uuid, "flow": "xtls-rprx-vision", "email": name + "@fixture.invalid", "enable": true}}, "decryption": "none"})
		stream, _ := json.Marshal(map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}})
		labPanelAction(t, client, "panel/api/inbounds/add", url.Values{"enable": {"true"}, "remark": {name}, "listen": {"127.0.0.1"}, "port": {strconv.Itoa(port)}, "protocol": {"vless"}, "settings": {string(settings)}, "streamSettings": {string(stream)}, "sniffing": {`{"enabled":false}`}})
	}
	create(inboundPort, "selected-existing")
	create(otherPort, "unselected-existing")
	if _, e = client.request(t.Context(), "panel/api/server/restartXrayService", url.Values{}); e != nil {
		t.Fatal("official encrypted inbound core startup")
	}
	original, e := client.Read(t.Context())
	if e != nil {
		t.Fatal("official actual inbounds read")
	}
	source, _ := ingress.FindSource("x-ui")
	report, e := ingress.Inspect(source, original.Rendered)
	if e != nil {
		t.Fatal("official generated config strict inspection")
	}
	selected := ""
	for _, row := range report.Inbounds {
		if row.Port == inboundPort && row.Candidate && row.Vision {
			selected = row.ID
		}
	}
	if selected == "" {
		t.Fatal("official TLS Vision inbound not a supported persistent reuse candidate")
	}
	target, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal("private business fixture")
	}
	defer target.Close()
	targetPort := target.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			connection, e := target.Accept()
			if e != nil {
				return
			}
			go func() {
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(6 * time.Second))
				io.Copy(connection, connection)
			}()
		}
	}()
	front, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal("Miku auth proxy fixture")
	}
	proxyPort := front.Addr().(*net.TCPAddr).Port
	proxyLife, stopProxy := context.WithCancel(t.Context())
	proxyDone := make(chan error, 1)
	var proxyCalls, authCalls atomic.Int32
	server := proxy.Server{Authenticate: func(user, pass string) bool {
		authCalls.Add(1)
		return user == "miku-private-user" && pass == "miku-private-secret"
	}, Dial: func(ctx context.Context, _, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || host != "business.invalid" || port != strconv.Itoa(targetPort) {
			return nil, errors.New("fixture target mismatch")
		}
		proxyCalls.Add(1)
		return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Addr().String())
	}}
	go func() { proxyDone <- server.Serve(proxyLife, front) }()
	proxyStopped := false
	t.Cleanup(func() {
		if !proxyStopped {
			stopProxy()
			<-proxyDone
		}
	})
	raw, plan, e := ingress.PreparePanelTemplate(original.Rendered, original.Template, Hash(original.Rendered), Hash(original.Template), selected, ingress.Proxy{Port: proxyPort, Username: "miku-private-user", Password: "miku-private-secret"})
	if e != nil {
		t.Fatal("actual official template candidate generation")
	}
	candidate, e := Canonical(raw)
	clear(raw)
	if e != nil {
		t.Fatal("candidate canonicalization")
	}
	if client.Save(t.Context(), Snapshot{Template: candidate, TestURL: original.TestURL}) != nil {
		t.Fatal("actual panel persistent route save")
	}
	applied := waitPanelRoute(t, client, original, plan)
	if !bytes.Equal(applied.Template, candidate) {
		t.Fatal("persistent template readback changed")
	}
	clientConfig := map[string]any{"log": map[string]any{"loglevel": "error"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": clientPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": inboundPort, "users": []any{map[string]any{"id": uuid, "flow": "xtls-rprx-vision", "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"serverName": "panel-ingress.test", "allowInsecure": false}}}}}
	clientPath := filepath.Join(root, "client.json")
	labWriteJSON(t, clientPath, clientConfig)
	clientEnv := append(env, "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+filepath.Join(root, "empty-ca-dir"))
	encrypted := startOfficial(t, xray, root, clientEnv, "run", "-c", clientPath)
	defer encrypted.stop()
	waitEcho(t, clientPort, targetPort, "business.invalid")
	if proxyCalls.Load() < 1 || authCalls.Load() < 1 {
		t.Fatal("encrypted client did not use real Miku authenticated frontend")
	}
	// Kill/restart the actual panel process, preserving the isolated on-disk DB.
	panel.stop()
	panel = nil
	panel = startOfficial(t, panelBinary, root, env)
	client = panelLoginEventually(t, endpoint, "miku-panel-lab", password)
	persisted := waitPanelRoute(t, client, original, plan)
	if !bytes.Equal(persisted.Template, candidate) {
		t.Fatal("panel restart lost persistent template")
	}
	waitEcho(t, clientPort, targetPort, "business.invalid")
	stopProxy()
	<-proxyDone
	proxyStopped = true
	if labEcho(clientPort, targetPort, "business.invalid") {
		t.Fatal("proxy stop had a direct fallback")
	}
	if client.Save(t.Context(), original) != nil {
		t.Fatal("real panel persistent rollback")
	}
	deadline := time.Now().Add(10 * time.Second)
	restored := false
	for time.Now().Before(deadline) {
		snapshot, e := client.Read(t.Context())
		if e == nil && snapshot.Same(original) {
			restored = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !restored {
		t.Fatal("official persistent rollback changed original clients/template")
	}
	t.Logf("OFFICIAL_PANEL_ROUTE_VERIFIED %s", `{"panel_version":"v3.9.0","ordinary_uid":true,"isolated_loopback_only":true,"real_login_csrf":true,"real_persistent_save":true,"panel_process_restart_preserved_route":true,"client_uuid_and_other_inbounds_preserved":true,"real_tls_vision_client_to_miku_authenticated_proxy":true,"proxy_stop_no_direct_fallback":true,"persistent_rollback_verified":true,"public_vpn_connected":false,"user_vps_modified":false}`)
}
