//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/gateway"
	"mikugate/internal/labexec"
	"mikugate/internal/store"
	"mikugate/internal/vpngate"
)

// The child command path is the real CLI, including the same private supervisor
// and worker entries as the release binary. There is no replacement VPN factory.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init", "serve", "instance", "vpn-executor", "_vpn-supervisor", "_vpn-openvpn", "_vpn-worker":
			if err := run(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

const runtimeLabUID = 65534
const runtimeLabVPNUID = 65533
const runtimeLabWorkerUID = 65532

// An anonymous network namespace contains the laboratory OpenVPN endpoint,
// fixed DNS fixture and HTTPS origin. None of its IPs/interfaces is added to the
// host. The real root executor then creates a second anonymous client namespace.
func TestLinuxVPNRuntimeControlPlane(t *testing.T) {
	if os.Getenv("MIKU_GATE_VPN_RUNTIME_TEST") != "1" {
		t.Skip("opt-in real Linux CLI VPN acceptance")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root coordinator is required")
	}
	binary := labexec.CopySelf(t)
	dataRoot := filepath.Join(filepath.Dir(binary), "full-runtime-lab")
	if err := os.Mkdir(dataRoot, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(dataRoot); err != nil {
			t.Error("test evidence retained", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 130*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestVPNRuntimeIsolatedHarness$", "-test.v", "-test.timeout=120s")
	child.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "GOMAXPROCS=1", "GOMEMLIMIT=96MiB", "MIKU_GATE_VPN_RUNTIME_HARNESS=" + dataRoot}
	child.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNET}
	child.Dir = "/"
	child.WaitDelay = 2 * time.Second
	var out labLog
	child.Stdout = &out
	child.Stderr = &out
	if err := child.Run(); err != nil {
		t.Fatalf("full isolated runtime failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "VPN_RUNTIME_FULL_PATH_OK") {
		t.Fatal("harness did not complete its explicit assertions")
	}
	t.Log("Observed: real CLI; UID65534 HTTP -> root VPN IPC -> UID65533 OpenVPN / UID65532 TUN-bound worker; actual TLS/DNS/business transfer; active mutation locks; wrong UID refusal; stop, controller SIGKILL, root daemon termination, persistence/restart and credential rotation. The endpoint, public-looking IPs and trust root are private fixtures, not public VPN evidence.")
}

type labLog struct {
	mu  sync.Mutex
	raw []byte
}

func (l *labLog) Write(raw []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.raw = append(l.raw, raw...)
	if len(l.raw) > 65536 {
		l.raw = append([]byte(nil), l.raw[len(l.raw)-65536:]...)
	}
	return len(raw), nil
}
func (l *labLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return string(l.raw) }

type labProcess struct {
	cmd  *exec.Cmd
	log  *labLog
	done chan error
	once sync.Once
}

func startLabProcess(t *testing.T, ctx context.Context, binary string, args []string, uid uint32, extraEnv ...string) *labProcess {
	t.Helper()
	var cmd *exec.Cmd
	if uid != 0 {
		all := []string{"--reuid=" + strconv.Itoa(int(uid)), "--regid=" + strconv.Itoa(int(uid)), "--clear-groups", "--no-new-privs", "--", binary}
		all = append(all, args...)
		cmd = exec.CommandContext(ctx, "/usr/bin/setpriv", all...)
	} else {
		cmd = exec.CommandContext(ctx, binary, args...)
	}
	cmd.Dir = "/"
	cmd.Env = append([]string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "GOMAXPROCS=1", "GOMEMLIMIT=128MiB"}, extraEnv...)
	cmd.WaitDelay = time.Second
	log := &labLog{}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal("start isolated test process", err)
	}
	p := &labProcess{cmd: cmd, log: log, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { p.stop(t, false) })
	return p
}
func (p *labProcess) stop(t *testing.T, kill bool) {
	t.Helper()
	p.once.Do(func() {
		if kill {
			p.cmd.Process.Kill()
		} else {
			p.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-p.done:
		case <-time.After(6 * time.Second):
			p.cmd.Process.Kill()
			select {
			case <-p.done:
			case <-time.After(time.Second):
				t.Error("owned laboratory process did not exit")
			}
		}
	})
}
func (p *labProcess) waitText(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.log.String(), text) {
			return
		}
		select {
		case err := <-p.done:
			p.once.Do(func() {})
			t.Fatalf("process exited before readiness: %v\n%s", err, p.log.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process readiness timed out\n%s", p.log.String())
}

type labPKI struct{ ca, server, serverKey, client, clientKey, origin, originKey string }

func runtimePKI(t *testing.T) labPKI {
	t.Helper()
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private full-runtime fixture root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pair := func(name string, id int64, usage x509.ExtKeyUsage, dns []string) (string, string) {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(id), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, DNSNames: dns}
		raw, err := x509.CreateCertificate(rand.Reader, cert, ca, &k.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}))
	}
	result := labPKI{ca: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	result.server, result.serverKey = pair("miku-runtime-vpn", 2, x509.ExtKeyUsageServerAuth, nil)
	result.client, result.clientKey = pair("miku-runtime-client", 3, x509.ExtKeyUsageClientAuth, nil)
	result.origin, result.originKey = pair("one.one.one.one", 4, x509.ExtKeyUsageServerAuth, []string{"one.one.one.one", "business.example"})
	return result
}
func runtimeLabServerConfig(p labPKI) string {
	return `local 8.8.8.8
port 11940
proto udp4
dev labvpn0
dev-type tun
topology subnet
server 10.244.0.0 255.255.255.0
verify-client-cert require
remote-cert-tls client
verify-x509-name miku-runtime-client name
dh none
tls-version-min 1.2
data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305
auth SHA256
allow-compression no
disable-dco
script-security 1
keepalive 2 6
verb 3
mute 0
<ca>
` + p.ca + "</ca>\n<cert>\n" + p.server + "</cert>\n<key>\n" + p.serverKey + "</key>\n"
}
func runtimeLabProfile(p labPKI) string {
	return "client\ndev tun\nproto udp4\nremote 8.8.8.8 11940\nverify-x509-name miku-runtime-vpn name\n<ca>\n" + p.ca + "</ca>\n<cert>\n" + p.client + "</cert>\n<key>\n" + p.clientKey + "</key>\n"
}
func exactLabFile(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fd.Write(raw); err != nil {
		fd.Close()
		t.Fatal(err)
	}
	if err = fd.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
}

func runtimeOrigin(t *testing.T, ctx context.Context, pki labPKI) (*atomic.Int32, *atomic.Int32) {
	t.Helper()
	var requests, badSource atomic.Int32
	observe := func(remote string) {
		host, _, _ := net.SplitHostPort(remote)
		if !strings.HasPrefix(host, "10.244.0.") {
			badSource.Add(1)
		}
	}
	answer := func(raw []byte) []byte {
		var q dnsmessage.Message
		if q.Unpack(raw) != nil || len(q.Questions) != 1 {
			return nil
		}
		m := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true}, Questions: q.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}}}}}
		packed, _ := m.Pack()
		return packed
	}
	udp, err := net.ListenPacket("udp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	go func() {
		var raw [4096]byte
		for {
			n, peer, err := udp.ReadFrom(raw[:])
			if err != nil {
				return
			}
			observe(peer.String())
			if data := answer(raw[:n]); data != nil {
				udp.WriteTo(data, peer)
			}
		}
	}()
	tcpDNS, err := net.Listen("tcp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tcpDNS.Close() })
	go func() {
		for {
			conn, err := tcpDNS.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				observe(conn.RemoteAddr().String())
				var length [2]byte
				if _, err := io.ReadFull(conn, length[:]); err != nil {
					return
				}
				size := binary.BigEndian.Uint16(length[:])
				if size < 12 || size > 4096 {
					return
				}
				raw := make([]byte, int(size))
				if _, err := io.ReadFull(conn, raw); err != nil {
					return
				}
				body := answer(raw)
				binary.BigEndian.PutUint16(length[:], uint16(len(body)))
				conn.Write(append(length[:], body...))
			}()
		}
	}()
	certificate, err := tls.X509KeyPair([]byte(pki.origin), []byte(pki.originKey))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp4", "93.184.216.34:443")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 3 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe(r.RemoteAddr)
		requests.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			badSource.Add(1)
		}
		if r.URL.Path == "/cdn-cgi/trace" {
			io.WriteString(w, "ip=93.184.216.34\nfixture=private-not-an-actual-egress\n")
			return
		}
		io.WriteString(w, "private end-to-end VPN response")
	})}
	t.Cleanup(func() { server.Close() })
	go server.Serve(tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}))
	return &requests, &badSource
}

type runtimeAPI struct {
	t          *testing.T
	base, csrf string
	cookie     *http.Cookie
}

func (api *runtimeAPI) call(method, path string, body any, expected int) []byte {
	api.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	request, err := http.NewRequest(method, api.base+"/api"+path, bytes.NewReader(raw))
	if err != nil {
		api.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if api.cookie != nil {
		request.AddCookie(api.cookie)
	}
	if method != "GET" {
		request.Header.Set("X-CSRF-Token", api.csrf)
	}
	client := &http.Client{Timeout: 40 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		api.t.Fatal("local API request failed", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		api.t.Fatal("bounded API read", err)
	}
	if response.StatusCode != expected {
		var fault struct {
			Code string `json:"code"`
		}
		json.Unmarshal(data, &fault)
		api.t.Fatalf("API %s %s got %d (%s), expected %d", method, path, response.StatusCode, fault.Code, expected)
	}
	if path == "/auth/login" && expected == 200 {
		cookies := response.Cookies()
		if len(cookies) != 1 {
			api.t.Fatal("missing session cookie")
		}
		api.cookie = cookies[0]
		var reply struct {
			CSRF string `json:"csrf"`
		}
		json.Unmarshal(data, &reply)
		api.csrf = reply.CSRF
	}
	return data
}
func runtimeController(t *testing.T, ctx context.Context, binary, data, ca, password string) (*labProcess, *runtimeAPI) {
	t.Helper()
	process := startLabProcess(t, ctx, binary, []string{"serve", "--data", data, "--listen", "127.0.0.1:0", "--vpn-executor", "--max-active", "1"}, runtimeLabUID, "SSL_CERT_FILE="+ca)
	process.waitText(t, "http://127.0.0.1:")
	match := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`).FindString(process.log.String())
	if match == "" {
		t.Fatal("no actual control listener")
	}
	api := &runtimeAPI{t: t, base: match}
	api.call("POST", "/auth/login", map[string]string{"username": "admin", "password": password}, 200)
	return process, api
}
func runtimeStart(t *testing.T, api *runtimeAPI, id string) gateway.State {
	t.Helper()
	raw := api.call("POST", "/exits/"+id+"/start", nil, 200)
	var state gateway.State
	if json.Unmarshal(raw, &state) != nil || state.Phase != "running" || !state.Healthy || !state.ProxyListening || !state.Proof.KernelVerified || !state.Proof.DNSVerified || state.Proof.Kind != "openvpn" || state.Proof.ExitIP != "93.184.216.34" {
		t.Fatal("missing real fixture VPN proof")
	}
	return state
}
func runtimeTunnel(t *testing.T, address, user, password string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	fmt.Fprintf(conn, "CONNECT business.example:443 HTTP/1.1\r\nHost: business.example:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || response.StatusCode != 200 {
		conn.Close()
		t.Fatal("authenticated VPN CONNECT failed", err)
	}
	return conn, reader
}
func runtimeBusiness(t *testing.T, address, user, password string, pki labPKI) {
	t.Helper()
	conn, reader := runtimeTunnel(t, address, user, password)
	defer conn.Close()
	if reader.Buffered() != 0 {
		t.Fatal("unexpected origin bytes before TLS")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(pki.ca)) {
		t.Fatal("test trust anchor")
	}
	secured := tls.Client(conn, &tls.Config{ServerName: "business.example", RootCAs: roots, MinVersion: tls.VersionTLS12})
	defer secured.Close()
	if err := secured.Handshake(); err != nil {
		t.Fatal("actual end-to-end TLS failed", err)
	}
	io.WriteString(secured, "GET / HTTP/1.1\r\nHost: business.example\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(secured), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != "private end-to-end VPN response" {
		t.Fatal("wrong full path response", err)
	}
}
func waitInactive(t *testing.T, api *runtimeAPI, id string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var state gateway.State
		raw := api.call("GET", "/exits/"+id+"/runtime", nil, 200)
		if json.Unmarshal(raw, &state) == nil && !state.ProxyListening && state.Phase != "stopping" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("listener state did not become inactive")
}
func waitSessionLeaseReleased(t *testing.T, api *runtimeAPI, id string) gateway.State {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		// Capabilities do not prove old session exit. Explicit start is idempotent on
		// the client but must eventually succeed after the old control lease closes.
		raw := api.call("GET", "/exits/"+id+"/runtime", nil, 200)
		var state gateway.State
		json.Unmarshal(raw, &state)
		if !state.ProxyListening {
			time.Sleep(100 * time.Millisecond)
			return runtimeStart(t, api, id)
		}
	}
	t.Fatal("lease did not release")
	return gateway.State{}
}

func TestVPNRuntimeIsolatedHarness(t *testing.T) {
	root := os.Getenv("MIKU_GATE_VPN_RUNTIME_HARNESS")
	if root == "" {
		t.Skip("private namespace coordinator only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("coordinator UID")
	}
	initial, err := net.Interfaces()
	if err != nil || len(initial) != 1 || initial[0].Name != "lo" {
		t.Fatal("harness must start in a new empty namespace")
	}
	for _, args := range [][]string{{"link", "set", "lo", "up"}, {"address", "add", "8.8.8.8/32", "dev", "lo"}, {"address", "add", "1.1.1.1/32", "dev", "lo"}, {"address", "add", "93.184.216.34/32", "dev", "lo"}} {
		cmd := exec.CommandContext(t.Context(), "/usr/sbin/ip", args...)
		if err := cmd.Run(); err != nil {
			t.Fatal("private fixture network", err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pki := runtimePKI(t)
	caPath := filepath.Join(root, "fixture-ca.pem")
	serverPath := filepath.Join(root, "fixture-server.conf")
	exactLabFile(t, caPath, []byte(pki.ca), 0644)
	exactLabFile(t, serverPath, []byte(runtimeLabServerConfig(pki)), 0600)
	requests, badSource := runtimeOrigin(t, t.Context(), pki)
	vpnServer := startLabProcess(t, t.Context(), "/usr/sbin/openvpn", []string{"--config", serverPath}, 0)
	vpnServer.waitText(t, "Initialization Sequence Completed")
	dataDir := filepath.Join(root, "data")
	if err = os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(dataDir, runtimeLabUID, runtimeLabUID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			t.Error(err)
			return
		}
		allowed := map[string]bool{"miku-gate.db": true, "miku-gate.db-wal": true, "miku-gate.db-shm": true, "miku-gate.db.key": true, ".serve.lock": true}
		for _, item := range entries {
			if !allowed[item.Name()] {
				t.Error("unknown data object retained")
				return
			}
			path := filepath.Join(dataDir, item.Name())
			info, e := os.Lstat(path)
			meta, ok := infoSys(info)
			if e != nil || !ok || !info.Mode().IsRegular() || meta.Uid != runtimeLabUID || meta.Nlink != 1 {
				t.Error("unsafe test data retained")
				return
			}
		}
		for _, item := range entries {
			if err = os.Remove(filepath.Join(dataDir, item.Name())); err != nil {
				t.Error(err)
				return
			}
		}
		if err = os.Remove(dataDir); err != nil {
			t.Error(err)
		}
	})
	init := exec.CommandContext(t.Context(), "/usr/bin/setpriv", "--reuid=65534", "--regid=65534", "--clear-groups", "--no-new-privs", "--", binary, "init", "--data", dataDir)
	init.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "GOMAXPROCS=1", "GOMEMLIMIT=96MiB"}
	raw, err := init.Output()
	if err != nil {
		t.Fatal("real unprivileged CLI init failed", err)
	}
	match := regexp.MustCompile(`密码：([^\r\n]+)`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("missing one-time credential")
	}
	password := string(match[1])
	clear(raw)
	database, err := store.Open(filepath.Join(dataDir, "miku-gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := database.InstanceID()
	if err != nil {
		t.Fatal(err)
	}
	profile := runtimeLabProfile(pki)
	node := domain.Node{ID: "full-runtime-private-fixture", Hostname: "miku-runtime-vpn", IP: "8.8.8.8", Country: "JP", Source: vpngate.Source, FetchedAt: time.Now().UTC(), Config: profile}
	if err = database.ReplaceNodes(t.Context(), []domain.Node{node}); err != nil {
		t.Fatal(err)
	}
	database.Close()
	// Seeded metadata is created before any service starts. Real API selection
	// still applies the complete source/time/profile/consent policy afterward.
	for _, name := range []string{"miku-gate.db", "miku-gate.db-wal", "miku-gate.db-shm"} {
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); err == nil {
			if err = os.Chown(path, runtimeLabUID, runtimeLabUID); err != nil {
				t.Fatal(err)
			}
		}
	}
	socket, _ := executoripc.VPNSocketPath(instance)
	t.Cleanup(func() {
		directory := filepath.Dir(socket)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Error(err)
			return
		}
		if len(entries) != 1 || entries[0].Name() != ".lock" {
			t.Error("VPN IPC lease objects retained for review")
			return
		}
		os.Remove(filepath.Join(directory, ".lock"))
		if err = os.Remove(directory); err != nil {
			t.Error(err)
		}
	})
	startDaemon := func() *labProcess {
		p := startLabProcess(t, t.Context(), binary, []string{"vpn-executor", "serve", "--instance", instance, "--allow-uid", "65534", "--tunnel-uid", "65533", "--worker-uid", "65532", "--apply"}, 0)
		p.waitText(t, "隔离VPN服务已监听")
		return p
	}
	daemon := startDaemon()
	denied := exec.CommandContext(t.Context(), binary, "vpn-executor", "info", "--instance", instance)
	if err = denied.Run(); err == nil {
		t.Fatal("root client was not refused by allow-UID policy")
	}
	control, api := runtimeController(t, t.Context(), binary, dataDir, caPath, password)
	caps := api.call("GET", "/vpn-executor", nil, 200)
	if !bytes.Contains(caps, []byte(`"reachable":true`)) {
		t.Fatal("separate root service not reached")
	}
	created := api.call("POST", "/exits", domain.ExitInput{Name: "Private full runtime", Country: "JP"}, 201)
	var exit struct {
		Exit     domain.Exit `json:"exit"`
		Password string      `json:"password"`
	}
	if json.Unmarshal(created, &exit) != nil || exit.Password == "" {
		t.Fatal("exit creation")
	}
	clear(created)
	prefix := "/exits/" + exit.Exit.ID
	selected := domain.VPNInput{NodeID: node.ID, PeerName: "miku-runtime-vpn", Confirm: true}
	bad := selected
	bad.PeerName = "wrong-server-name"
	api.call("PUT", prefix+"/vpn", bad, 400)
	api.call("PUT", prefix+"/vpn", selected, 204)
	view := api.call("GET", prefix+"/vpn", nil, 200)
	if bytes.Contains(view, []byte("PRIVATE KEY")) || bytes.Contains(view, []byte("BEGIN CERTIFICATE")) {
		t.Fatal("profile leaked into API view")
	}
	state := runtimeStart(t, api, exit.Exit.ID)
	runtimeBusiness(t, state.Listen, exit.Exit.Username, exit.Password, pki)
	api.call("PUT", prefix+"/vpn", selected, 409)
	api.call("POST", prefix+"/credentials", nil, 409)
	api.call("DELETE", prefix, nil, 409)
	api.call("POST", prefix+"/probe", nil, 200)
	held, _ := runtimeTunnel(t, state.Listen, exit.Exit.Username, exit.Password)
	api.call("POST", prefix+"/stop", nil, 204)
	waitInactive(t, api, exit.Exit.ID)
	held.SetReadDeadline(time.Now().Add(2 * time.Second))
	var byteOne [1]byte
	if _, err = held.Read(byteOne[:]); err == nil {
		t.Fatal("existing connection survived explicit stop")
	}
	held.Close()
	state = runtimeStart(t, api, exit.Exit.ID)
	runtimeBusiness(t, state.Listen, exit.Exit.Username, exit.Password, pki)
	control.stop(t, true)
	deadline := time.Now().Add(4 * time.Second)
	for {
		conn, e := net.DialTimeout("tcp4", state.Listen, 100*time.Millisecond)
		if e != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("killed web process left listener alive")
		}
		time.Sleep(20 * time.Millisecond)
	}
	control, api = runtimeController(t, t.Context(), binary, dataDir, caPath, password)
	counts := api.call("GET", "/overview", nil, 200)
	if !bytes.Contains(counts, []byte(`"active_count":0`)) {
		t.Fatal("restart fabricated an active tunnel")
	}
	state = waitSessionLeaseReleased(t, api, exit.Exit.ID)
	runtimeBusiness(t, state.Listen, exit.Exit.Username, exit.Password, pki)
	held, _ = runtimeTunnel(t, state.Listen, exit.Exit.Username, exit.Password)
	daemon.stop(t, false)
	waitInactive(t, api, exit.Exit.ID)
	held.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err = held.Read(byteOne[:]); err == nil {
		t.Fatal("root service exit did not revoke stream")
	}
	held.Close()
	daemon = startDaemon()
	state = runtimeStart(t, api, exit.Exit.ID)
	runtimeBusiness(t, state.Listen, exit.Exit.Username, exit.Password, pki)
	api.call("POST", prefix+"/stop", nil, 204)
	rotated := api.call("POST", prefix+"/credentials", nil, 200)
	var secret struct {
		Password string `json:"password"`
	}
	if json.Unmarshal(rotated, &secret) != nil || secret.Password == "" || secret.Password == exit.Password {
		t.Fatal("credential rotation not applied")
	}
	clear(rotated)
	state = runtimeStart(t, api, exit.Exit.ID)
	runtimeBusiness(t, state.Listen, exit.Exit.Username, secret.Password, pki)
	conn, err := net.DialTimeout("tcp4", state.Listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	auth := base64.StdEncoding.EncodeToString([]byte(exit.Exit.Username + ":" + exit.Password))
	fmt.Fprintf(conn, "CONNECT business.example:443 HTTP/1.1\r\nHost: business.example:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	conn.Close()
	if err != nil || response.StatusCode != 407 {
		t.Fatal("old proxy credential survived rotation", err)
	}
	api.call("POST", prefix+"/stop", nil, 204)
	api.call("DELETE", prefix+"/vpn", nil, 204)
	api.call("DELETE", prefix, nil, 204)
	control.stop(t, false)
	daemon.stop(t, false)
	vpnServer.stop(t, false)
	if requests.Load() < 10 || badSource.Load() != 0 {
		t.Fatal("insufficient tunnel-origin evidence", requests.Load(), badSource.Load())
	}
	fmt.Printf("VPN_RUNTIME_FULL_PATH_OK business_requests=%d non_tunnel_sources=%d\n", requests.Load(), badSource.Load())
}
func infoSys(info os.FileInfo) (*syscall.Stat_t, bool) {
	if info == nil {
		return nil, false
	}
	meta, ok := info.Sys().(*syscall.Stat_t)
	return meta, ok
}
