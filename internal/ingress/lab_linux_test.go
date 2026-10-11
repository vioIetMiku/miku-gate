//go:build linux

package ingress

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"mikugate/internal/proxy"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const labUUID = "8ed34584-3589-4d8b-88f3-e4c3f56cae63"

func reservePort(t *testing.T) int {
	t.Helper()
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
func labWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func labCertificates(t *testing.T, dir string) (string, string, string) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Miku private ingress lab CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(72 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	leafKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"ingress.test"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := x509.MarshalPKCS8PrivateKey(leafKey)
	if e != nil {
		t.Fatal(e)
	}
	certFile, keyFile, caFile := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key"), filepath.Join(dir, "ca.pem")
	for path, data := range map[string][]byte{certFile: append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), caFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})} {
		if e = os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return certFile, keyFile, caFile
}
func labServerConfig(core string, port, other int, cert, key string) map[string]any {
	if core == "xray" {
		inbound := func(tag string, p int) any {
			return map[string]any{"tag": tag, "listen": "127.0.0.1", "port": p, "protocol": "vless", "settings": map[string]any{"decryption": "none", "clients": []any{map[string]any{"id": labUUID, "flow": "xtls-rprx-vision"}}}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}
		}
		return map[string]any{"log": map[string]any{"loglevel": "error"}, "inbounds": []any{inbound("selected", port), inbound("unselected", other)}, "outbounds": []any{map[string]any{"tag": "old-direct", "protocol": "freedom"}}, "routing": map[string]any{"rules": []any{map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "old-direct"}}}}
	}
	inbound := func(tag string, p int) any {
		return map[string]any{"tag": tag, "type": "vless", "listen": "127.0.0.1", "listen_port": p, "users": []any{map[string]any{"uuid": labUUID, "flow": "xtls-rprx-vision"}}, "tls": map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}}
	}
	return map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{inbound("selected", port), inbound("unselected", other)}, "outbounds": []any{map[string]any{"tag": "old-direct", "type": "direct"}}, "route": map[string]any{"rules": []any{map[string]any{"network": []string{"tcp", "udp"}, "action": "route", "outbound": "old-direct"}}}}
}
func labClientConfig(core string, port, server int) map[string]any {
	if core == "xray" {
		return map[string]any{"log": map[string]any{"loglevel": "error"}, "inbounds": []any{map[string]any{"tag": "client-socks", "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": server, "users": []any{map[string]any{"id": labUUID, "encryption": "none", "flow": "xtls-rprx-vision"}}}}}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": "ingress.test", "allowInsecure": false}}}}}
	}
	return map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{map[string]any{"type": "vless", "server": "127.0.0.1", "server_port": server, "uuid": labUUID, "flow": "xtls-rprx-vision", "tls": map[string]any{"enabled": true, "server_name": "ingress.test", "insecure": false}}}}
}

type coreDiagnostics struct {
	mu     sync.Mutex
	counts map[string]int
	bytes  int
}

func (d *coreDiagnostics) Write(raw []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytes += len(raw)
	// Tokens are a closed vocabulary from protocol/runtime diagnostics, not raw
	// log words (which may be encoded credentials). Case-independent matching.
	lower := strings.ToLower(string(raw))
	for _, token := range []string{"argument", "socket", "operation", "permission", "certificate", "cipher", "finished", "padding", "decryption", "signature", "traffic", "unsupported", "fingerprint", "peer", "handshake", "compress", "user", "client", "server", "public key", "private key", "authentication", "connection", "invalid", "timeout", "refused", "alert", "record", "packet", "protocol", "bad", "read", "write", "retry", "closed"} {
		if strings.Contains(lower, token) {
			if d.counts == nil {
				d.counts = map[string]int{}
			}
			d.counts["token_"+strings.ReplaceAll(token, " ", "_")]++
		}
	}

	if d.counts == nil {
		d.counts = map[string]int{}
	}
	// fixed categories only; no raw logs, keys, UUIDs or configs enter CI output.
	for _, item := range []struct{ needle, label string }{{"REALITY verification failed", "reality_verification_failed"}, {"processed invalid connection", "invalid_connection"}, {"failed to read", "read_failed"}, {"failed to connect", "connect_failed"}, {"authentication", "authentication_error"}, {"TLS", "tls_log"}, {"REALITY", "reality_log"}, {"connection refused", "connection_refused"}, {"EOF", "eof"}, {"timeout", "timeout"}, {"invalid", "invalid_input"}, {"failed", "failure_log"}, {"payload[0]", "reality_record_padding"}, {"invalid key", "invalid_key"}, {"invalid user", "invalid_user"}, {"invalid flow", "invalid_flow"}, {"invalid response", "invalid_response"}, {"invalid signature", "invalid_signature"}, {"invalid padding", "invalid_padding"}, {"target sent incorrect", "target_serverhello_incomplete"}, {"handshake did not complete", "target_handshake_incomplete"}, {"server name mismatch", "server_name_mismatch"}, {"validation criteria not met", "reality_authentication_rejected"}} {
		if strings.Contains(string(raw), item.needle) {
			d.counts[item.label]++
		}
	}
	return len(raw), nil
}
func (d *coreDiagnostics) Summary() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	labels := []string{}
	for label, count := range d.counts {
		labels = append(labels, fmt.Sprintf("%s=%d", label, count))
	}
	sort.Strings(labels)
	return fmt.Sprintf("bytes=%d categories=%s", d.bytes, strings.Join(labels, ","))
}

type coreProcess struct {
	diagnostics *coreDiagnostics
	cmd         *exec.Cmd
	once        sync.Once
	done        chan error
}

func coreCommand(ctx context.Context, binary, core, config string, check bool) *exec.Cmd {
	args := []string{"run", "-c", config}
	if core == "xray" {
		args = []string{"run", "-config", config}
		if check {
			args = []string{"run", "-test", "-config", config}
		}
	} else if check {
		args = []string{"check", "-c", config}
	}
	c := exec.CommandContext(ctx, binary, args...)
	c.Dir = filepath.Dir(config)
	c.WaitDelay = time.Second
	return c
}
func startCore(t *testing.T, binary, core, config, ca string, port int) *coreProcess {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := coreCommand(ctx, binary, core, config, false)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB", "SSL_CERT_FILE=" + ca, "SSL_CERT_DIR=" + filepath.Dir(ca) + "/empty-cert-directory"}
	diag := new(coreDiagnostics)
	cmd.Stdout = diag
	cmd.Stderr = diag
	p := &coreProcess{diagnostics: diag, cmd: cmd, done: make(chan error, 1)}
	if e := cmd.Start(); e != nil {
		cancel()
		t.Fatal("core start failed")
	}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { p.once.Do(func() { cancel(); <-p.done }) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if e == nil {
			c.Close()
			return p
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("core did not listen in private lab")
	return nil
}
func socksConnect(t *testing.T, port int, host string, target int) (net.Conn, error) {
	t.Helper()
	c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if e != nil {
		return nil, e
	}
	// Xray's REALITY target cold-start record preflight waits up to five seconds.
	// A two-second client fixture deadline races initialization, not TLS trust.
	c.SetDeadline(time.Now().Add(8 * time.Second))
	fail := func(e error) (net.Conn, error) { c.Close(); return nil, e }
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return fail(e)
	}
	var response [2]byte
	if _, e = io.ReadFull(c, response[:]); e != nil || response != [2]byte{5, 0} {
		return fail(fmt.Errorf("SOCKS method"))
	}
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	request = append(request, byte(target>>8), byte(target))
	if _, e = c.Write(request); e != nil {
		return fail(e)
	}
	var reply [4]byte
	if _, e = io.ReadFull(c, reply[:]); e != nil || reply[1] != 0 {
		return fail(fmt.Errorf("SOCKS connect rejected"))
	}
	n := 0
	switch reply[3] {
	case 1:
		n = 6
	case 4:
		n = 18
	case 3:
		var length [1]byte
		if _, e = io.ReadFull(c, length[:]); e != nil {
			return fail(e)
		}
		n = int(length[0]) + 2
	default:
		return fail(fmt.Errorf("SOCKS reply"))
	}
	if _, e = io.CopyN(io.Discard, c, int64(n)); e != nil {
		return fail(e)
	}
	return c, nil
}
func echoThrough(t *testing.T, port int, host string, target int) bool {
	t.Helper()
	c, e := socksConnect(t, port, host, target)
	if e != nil {
		return false
	}
	defer c.Close()
	message := []byte("miku-encrypted-ingress-route")
	if _, e = c.Write(message); e != nil {
		return false
	}
	reply := make([]byte, len(message))
	_, e = io.ReadFull(c, reply)
	return e == nil && bytes.Equal(reply, message)
}
func udpThrough(t *testing.T, port int, target *net.UDPAddr) bool {
	t.Helper()
	c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if e != nil {
		return false
	}
	defer c.Close()
	// Xray's REALITY target cold-start record preflight waits up to five seconds.
	// A two-second client fixture deadline races initialization, not TLS trust.
	c.SetDeadline(time.Now().Add(8 * time.Second))
	c.Write([]byte{5, 1, 0})
	var method [2]byte
	if _, e = io.ReadFull(c, method[:]); e != nil || method != [2]byte{5, 0} {
		return false
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	var head [4]byte
	if _, e = io.ReadFull(c, head[:]); e != nil || head[1] != 0 {
		return false
	}
	var address []byte
	switch head[3] {
	case 1:
		address = make([]byte, 6)
	case 4:
		address = make([]byte, 18)
	default:
		return false
	}
	if _, e = io.ReadFull(c, address); e != nil {
		return false
	}
	relay := &net.UDPAddr{IP: net.IP(address[:len(address)-2]), Port: int(binary.BigEndian.Uint16(address[len(address)-2:]))}
	if relay.IP.IsUnspecified() {
		relay.IP = net.ParseIP("127.0.0.1")
	}
	u, e := net.DialUDP("udp", nil, relay)
	if e != nil {
		return false
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(700 * time.Millisecond))
	packet := []byte{0, 0, 0, 1}
	packet = append(packet, target.IP.To4()...)
	packet = append(packet, byte(target.Port>>8), byte(target.Port))
	packet = append(packet, []byte("udp-proof")...)
	u.Write(packet)
	reply := make([]byte, 128)
	n, e := u.Read(reply)
	return e == nil && n >= 10 && string(reply[n-9:n]) == "udp-proof"
}
func TestLinuxIngressCoreRouting(t *testing.T) {
	if os.Getenv("MIKU_GATE_INGRESS_TEST") != "1" {
		t.Skip("explicit isolated real-core lab required")
	}
	xray, sing := os.Getenv("MIKU_GATE_XRAY"), os.Getenv("MIKU_GATE_SINGBOX")
	if !filepath.IsAbs(xray) || !filepath.IsAbs(sing) {
		t.Fatal("exact core binary paths required")
	}
	links, e := net.Interfaces()
	if e != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("private loopback-only namespace required")
	}
	for _, tc := range []struct{ core, binary string }{{"xray", xray}, {"sing-box", sing}} {
		t.Run(tc.core, func(t *testing.T) {
			dir := t.TempDir()
			cert, key, ca := labCertificates(t, dir)
			var targetCalls, proxyCalls, authCalls, udpCalls atomic.Int32
			target, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer target.Close()
			targetPort := target.Addr().(*net.TCPAddr).Port
			go func() {
				for {
					c, e := target.Accept()
					if e != nil {
						return
					}
					targetCalls.Add(1)
					go func() { defer c.Close(); c.SetDeadline(time.Now().Add(4 * time.Second)); io.Copy(c, c) }()
				}
			}()
			udp, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
			if e != nil {
				t.Fatal(e)
			}
			defer udp.Close()
			go func() {
				b := make([]byte, 1024)
				for {
					n, from, e := udp.ReadFromUDP(b)
					if e != nil {
						return
					}
					udpCalls.Add(1)
					udp.WriteToUDP(b[:n], from)
				}
			}()
			proxyListener, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			proxyPort := proxyListener.Addr().(*net.TCPAddr).Port
			proxyCtx, stopProxy := context.WithCancel(context.Background())
			proxyDone := make(chan error, 1)
			server := &proxy.Server{Authenticate: func(u, p string) bool { authCalls.Add(1); return u == "lab-user" && p == "lab-secret" }, Dial: func(ctx context.Context, n, address string) (net.Conn, error) {
				proxyCalls.Add(1)
				host, port, e := net.SplitHostPort(address)
				if e != nil || port != strconv.Itoa(targetPort) || (host != "lab-target.invalid" && host != "127.0.0.1") {
					return nil, fmt.Errorf("target outside private proof")
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Addr().String())
			}}
			go func() { proxyDone <- server.Serve(proxyCtx, proxyListener) }()
			t.Cleanup(func() { stopProxy(); <-proxyDone })
			selectedPort, otherPort, clientPort, otherClientPort := reservePort(t), reservePort(t), reservePort(t), reservePort(t)
			source := Source{ID: "private-fixture", Core: tc.core, Path: "private-fixture.json"}
			config, _ := json.Marshal(labServerConfig(tc.core, selectedPort, otherPort, cert, key))
			summary, e := Inspect(source, config)
			if e != nil {
				t.Fatal(e)
			}
			candidate, plan, e := Prepare(source, config, summary.SHA256, summary.Inbounds[0].ID, Proxy{proxyPort, "lab-user", "lab-secret"})
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "server.json")
			if e = os.WriteFile(path, candidate, 0600); e != nil {
				t.Fatal(e)
			}
			check := coreCommand(context.Background(), tc.binary, tc.core, path, true)
			if e = check.Run(); e != nil {
				t.Fatal("actual core rejected generated candidate")
			}
			startCore(t, tc.binary, tc.core, path, ca, selectedPort)
			cp, op := filepath.Join(dir, "client.json"), filepath.Join(dir, "other-client.json")
			labWrite(t, cp, labClientConfig(tc.core, clientPort, selectedPort))
			labWrite(t, op, labClientConfig(tc.core, otherClientPort, otherPort))
			startCore(t, tc.binary, tc.core, cp, ca, clientPort)
			startCore(t, tc.binary, tc.core, op, ca, otherClientPort)
			if !echoThrough(t, clientPort, "lab-target.invalid", targetPort) || proxyCalls.Load() != 1 || authCalls.Load() != 1 {
				t.Fatal("TLS Vision selected path did not use authenticated Miku frontend with remote DNS host")
			}
			if !echoThrough(t, otherClientPort, "127.0.0.1", targetPort) || proxyCalls.Load() != 1 {
				t.Fatal("unselected ingress behavior changed")
			}
			if udpThrough(t, clientPort, udp.LocalAddr().(*net.UDPAddr)) || udpCalls.Load() != 0 {
				t.Fatal("selected UDP bypassed explicit drop")
			}
			// Some Vision clients reject UDP/443; non-443 fixture plus unselected positive
			// control proves UDP actually works before attributing a failure to our rule.
			if !udpThrough(t, otherClientPort, udp.LocalAddr().(*net.UDPAddr)) || udpCalls.Load() != 1 {
				t.Fatal("UDP positive control unavailable")
			}
			before := targetCalls.Load()
			stopProxy()
			proxyListener.Close()
			if echoThrough(t, clientPort, "127.0.0.1", targetPort) || targetCalls.Load() != before {
				t.Fatal("selected path fell back to old direct after proxy failure")
			}
			if !echoThrough(t, otherClientPort, "127.0.0.1", targetPort) {
				t.Fatal("unselected path broken on Miku stop")
			}
			if !plan.UDPBlocked {
				t.Fatal("missing plan protection")
			}
			version := exec.Command(tc.binary, "version")
			v, e := version.Output()
			if e != nil {
				t.Fatal("core version unavailable")
			}
			first := strings.SplitN(string(v), "\n", 2)[0]
			if len(first) > 200 {
				t.Fatal("version bound")
			}
			t.Logf("Observed %s: actual config check; two preserved TLS/Vision VLESS entrances, private CA verified (no insecure flags), selected TCP through real Miku authenticated SOCKS frontend with unresolved hostname, unselected direct positive control, selected UDP drop with unselected UDP success, stopped proxy no direct fallback. Core: %s", tc.core, first)
		})
	}
}
