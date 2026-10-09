//go:build linux

package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"mikugate/internal/labexec"
)

const transportServer = "198.18.48.1"
const transportClient = "198.18.48.2"
const targetIP = "198.19.0.9"
const tunnelServer = "10.243.0.1"
const dnsPort = "15353"
const targetPort = "18080"

type labNamespace struct {
	name     string
	file     *os.File
	identity NamespaceID
}
type vpnLab struct {
	t                        *testing.T
	ctx                      context.Context
	binary, ip, nft, nsenter string
	client, server           labNamespace
	table                    string
	host                     NamespaceID
}

func newVPNLab(t *testing.T) *vpnLab {
	t.Helper()
	if os.Getenv("MIKU_GATE_OPENVPN_TEST") != "1" {
		t.Skip("controlled OpenVPN integration is opt-in: MIKU_GATE_OPENVPN_TEST=1")
	}
	if os.Geteuid() != 0 {
		t.Fatal("opted-in OpenVPN lab requires root in a disposable Linux environment")
	}
	if info, err := os.Stat("/dev/net/tun"); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Fatal("real TUN character device required", err)
	}
	var nonce [10]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(nonce[:])
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Second)
	t.Cleanup(cancel)
	l := &vpnLab{t: t, ctx: ctx, table: "mgv_" + id[:12]}
	var err error
	if l.host, err = CurrentNamespace(); err != nil {
		t.Fatal(err)
	}
	if l.ip, err = trustedTool("ip"); err != nil {
		t.Fatal(err)
	}
	if l.nft, err = trustedTool("nft"); err != nil {
		t.Fatal(err)
	}
	if l.nsenter, err = trustedTool("nsenter"); err != nil {
		t.Fatal(err)
	}
	l.binary = labexec.CopySelf(t)
	create := func(name string) labNamespace {
		l.hostCommand(l.ip, "netns", "add", name)
		fd, err := unix.Open(filepath.Join("/run/netns", name), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(fd), name)
		identity, err := namespaceFileID(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		space := labNamespace{name: name, file: file, identity: identity}
		t.Cleanup(func() { l.cleanupNamespace(space); file.Close() })
		return space
	}
	l.client = create("mgvc-" + id)
	l.server = create("mgvs-" + id)
	return l
}
func boundedCommand(ctx context.Context, binary string, args []string, input string, files ...*os.File) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = strings.NewReader(input)
	cmd.ExtraFiles = files
	cmd.WaitDelay = time.Second
	output := &processOutput{ready: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(binary), err, output.text())
	}
	return []byte(output.text()), nil
}
func (l *vpnLab) hostCommand(binary string, args ...string) []byte {
	l.t.Helper()
	raw, err := boundedCommand(l.ctx, binary, args, "")
	if err != nil {
		l.t.Fatal(err)
	}
	return raw
}
func (l *vpnLab) command(space labNamespace, binary string, args ...string) []byte {
	l.t.Helper()
	return l.input(space, binary, "", args...)
}
func (l *vpnLab) input(space labNamespace, binary, input string, args ...string) []byte {
	l.t.Helper()
	command := append([]string{"--net=/proc/self/fd/3", "--", binary}, args...)
	raw, err := boundedCommand(l.ctx, l.nsenter, command, input, space.file)
	if err != nil {
		l.t.Fatal(err)
	}
	return raw
}
func (l *vpnLab) cleanupNamespace(space labNamespace) {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Verify the persistent name still references exactly the created namespace.
	fd, err := unix.Open(filepath.Join("/run/netns", space.name), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		l.t.Errorf("lab namespace retained: %v", err)
		return
	}
	file := os.NewFile(uintptr(fd), space.name)
	defer file.Close()
	identity, err := namespaceFileID(file)
	if err != nil || identity != space.identity {
		l.t.Errorf("refusing to delete replaced lab namespace %s", space.name)
		return
	}
	pids, err := boundedCommand(ctx, l.ip, []string{"netns", "pids", space.name}, "")
	if err != nil || len(bytes.TrimSpace(pids)) != 0 {
		l.t.Errorf("busy lab namespace retained: %s", space.name)
		return
	}
	args := []string{"--net=/proc/self/fd/3", "--", l.ip, "-j", "link", "show"}
	raw, err := boundedCommand(ctx, l.nsenter, args, "", file)
	if err != nil {
		l.t.Error(err)
		return
	}
	var links []struct {
		Name string `json:"ifname"`
	}
	if json.Unmarshal(raw, &links) != nil {
		l.t.Error("invalid cleanup link list")
		return
	}
	allowed := map[string]bool{"lo": true, "mgc0": true, "mgs0": true, Device: true, "mgstun0": true, "mgservice0": true}
	for _, link := range links {
		if !allowed[link.Name] {
			l.t.Errorf("foreign interface retained: %s", link.Name)
			return
		}
	}
	if _, err = boundedCommand(ctx, l.ip, []string{"netns", "delete", space.name}, ""); err != nil {
		l.t.Error(err)
	}
}
func (l *vpnLab) hostSnapshot() []byte {
	l.t.Helper()
	var entries []any
	for _, query := range []struct {
		tool string
		args []string
	}{{l.ip, []string{"-j", "link", "show"}}, {l.ip, []string{"-j", "route", "show", "table", "all"}}, {l.ip, []string{"-j", "-6", "route", "show", "table", "all"}}, {l.nft, []string{"-j", "list", "ruleset"}}} {
		raw := l.hostCommand(query.tool, query.args...)
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			l.t.Fatal(err)
		}
		stripCounts(value)
		entries = append(entries, value)
	}
	dns, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		l.t.Fatal(err)
	}
	entries = append(entries, string(dns))
	raw, err := json.Marshal(entries)
	if err != nil {
		l.t.Fatal(err)
	}
	return raw
}
func stripCounts(value any) {
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			stripCounts(child)
		}
	case map[string]any:
		delete(v, "expires")
		if counter, ok := v["counter"].(map[string]any); ok {
			delete(counter, "bytes")
			delete(counter, "packets")
		}
		for _, child := range v {
			stripCounts(child)
		}
	}
}
func (l *vpnLab) installTopology() {
	// The first transaction denies all IPv6, forwarding and unlisted IPv4. No veth
	// exists until it commits, and the pair is created wholly inside the two labs.
	rules := fmt.Sprintf(`table inet %s {
 comment "miku-controlled-openvpn-lab"
 chain input { type filter hook input priority -300; policy drop;
  meta nfproto ipv6 counter drop
  iifname "lo" accept
  iifname "%s" accept
  iifname "mgc0" ip saddr %s udp sport 11940 accept
  counter drop comment "deny-input"
 }
 chain output { type filter hook output priority -300; policy drop;
  meta nfproto ipv6 counter drop
  oifname "lo" accept
  oifname "%s" accept
  oifname "mgc0" ip daddr %s udp dport 11940 accept
  counter drop comment "deny-output"
 }
 chain forward { type filter hook forward priority -300; policy drop; counter drop; }
}
`, l.table, Device, transportServer, Device, transportServer)
	l.input(l.client, l.nft, rules, "-f", "-")
	l.command(l.server, l.ip, "link", "add", "mgs0", "type", "veth", "peer", "name", "mgc0", "netns", l.client.name)
	l.command(l.server, l.ip, "address", "add", transportServer+"/30", "dev", "mgs0")
	l.command(l.client, l.ip, "address", "add", transportClient+"/30", "dev", "mgc0")
	l.command(l.server, l.ip, "link", "set", "mgs0", "up")
	l.command(l.client, l.ip, "link", "set", "mgc0", "up")
	l.command(l.server, l.ip, "link", "set", "lo", "up")
	l.command(l.client, l.ip, "link", "set", "lo", "up")
	l.command(l.server, l.ip, "link", "add", "mgservice0", "type", "dummy")
	l.command(l.server, l.ip, "address", "add", targetIP+"/32", "dev", "mgservice0")
	l.command(l.server, l.ip, "link", "set", "mgservice0", "up")
}
func serverConfig(p testIdentity) string {
	return `local 198.18.48.1
port 11940
proto udp4
dev mgstun0
dev-type tun
topology subnet
server 10.243.0.0 255.255.255.0
verify-client-cert require
remote-cert-tls client
verify-x509-name miku-lab-client name
dh none
tls-version-min 1.2
data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305
auth SHA256
allow-compression no
disable-dco
script-security 1
keepalive 2 6
push "redirect-gateway def1"
push "dhcp-option DNS 198.18.48.1"
push "route 203.0.113.0 255.255.255.0"
verb 3
mute 0
<ca>
` + p.ca + "</ca>\n<cert>\n" + p.serverCert + "</cert>\n<key>\n" + p.serverKey + "</key>\n"
}
func (l *vpnLab) clientProfile(pki testIdentity) Profile {
	l.t.Helper()
	profile, err := normalize(pki.profile(transportServer), transportServer, time.Now(), func(ip string) error {
		if ip != transportServer {
			return ErrProfile
		}
		return nil
	})
	if err != nil {
		l.t.Fatal(err)
	}
	return profile
}
func (l *vpnLab) ready(p *process) {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(l.ctx, 12*time.Second)
	defer cancel()
	if err := p.waitReady(ctx); err != nil {
		l.t.Fatalf("OpenVPN readiness failed: %v\n%s", err, p.output.text())
	}
}
func (l *vpnLab) outputDrops() uint64 {
	raw := l.command(l.client, l.nft, "-j", "list", "table", "inet", l.table)
	var doc struct {
		NFTables []struct {
			Rule *struct {
				Comment string
				Expr    []struct{ Counter *struct{ Packets uint64 } }
			}
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		l.t.Fatal(err)
	}
	for _, item := range doc.NFTables {
		if item.Rule != nil && item.Rule.Comment == "deny-output" {
			for _, expr := range item.Rule.Expr {
				if expr.Counter != nil {
					return expr.Counter.Packets
				}
			}
		}
	}
	l.t.Fatal("missing default-drop counter")
	return 0
}
func TestLinuxOpenVPNControlledTunnelAndFailClosed(t *testing.T) {
	runControlledTunnel(t, false)
}
func TestLinuxOpenVPNLegacyControlledTunnelAndFailClosed(t *testing.T) {
	runControlledTunnel(t, true)
}
func runControlledTunnel(t *testing.T, legacy bool) {
	l := newVPNLab(t)
	before := l.hostSnapshot()
	pki := testPKI(t)
	l.installTopology()
	serverText := serverConfig(pki)
	if legacy {
		serverText = strings.Replace(serverText, "data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305", "data-ciphers AES-128-CBC", 1)
		serverText = strings.Replace(serverText, "auth SHA256", "auth SHA1", 1)
	}
	server, err := startProcess(l.ctx, l.server.file, serverText)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.close)
	l.ready(server)
	profile := l.clientProfile(pki)
	if legacy {
		profile, err = normalizeOptions(pki.profile(transportServer)+"cipher AES-128-CBC\ndata-ciphers AES-128-CBC\nauth SHA1\n", transportServer, time.Now(), func(ip string) error {
			if ip != transportServer {
				return ErrProfile
			}
			return nil
		}, NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: "miku-lab-server"})
		if err != nil || !profile.LegacyDataChannel() {
			t.Fatal("explicit legacy profile failed", err)
		}
	}
	// Certificate-role checks alone are not evidence of actual peer authentication.
	// A deliberately wrong expected server name must fail before tunnel setup.
	wrong := profile
	wrong.peerName = "wrong-server-name"
	failed, err := startClient(l.ctx, l.client.file, wrong)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(failed.close)
	denied, cancel := context.WithTimeout(l.ctx, 2*time.Second)
	err = failed.waitReady(denied)
	cancel()
	if err == nil {
		t.Fatal("wrong peer certificate name reached initialized state")
	}
	failed.close()
	if !strings.Contains(failed.output.text(), "VERIFY X509NAME ERROR") {
		t.Fatalf("missing authentication rejection evidence: %s", failed.output.text())
	}
	client, err := startClient(l.ctx, l.client.file, profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.close)
	l.ready(client)
	routes := l.command(l.client, l.ip, "-j", "route", "show", "table", "all")
	var observedRoutes []struct {
		Destination string `json:"dst"`
	}
	if json.Unmarshal(routes, &observedRoutes) != nil {
		t.Fatal("invalid route snapshot")
	}
	for _, route := range observedRoutes {
		if route.Destination == "default" || strings.HasPrefix(route.Destination, "203.0.113.") {
			t.Fatal("pushed routes were applied")
		}
	}
	l.command(l.client, l.ip, "route", "replace", "default", "dev", Device)
	fixture := l.startFixture("server", l.server)
	t.Cleanup(fixture.close)
	fixture.wait("SERVICE_READY")
	l.runProbe("success")
	l.runProbe("policy")
	l.runProbe("dns-timeout")
	stats := fixture.stats()
	if stats.DNSUDP < 3 || stats.DNSTCP < 1 || stats.Messages < 3 || stats.BadSource != 0 {
		t.Fatalf("insufficient tunnel DNS/TCP evidence: %+v", stats)
	}
	held := l.startFixture("held-client", l.client)
	t.Cleanup(held.close)
	held.wait("HOLD_READY")
	oldStats := fixture.stats()
	drops := l.outputDrops()
	client.close()
	// Explicitly leave a tempting but forbidden underlay default route. Even raw
	// sockets in this private namespace must not bypass the tunnel after failure.
	l.command(l.client, l.ip, "route", "replace", "default", "via", transportServer, "dev", "mgc0")
	held.send("CHECK")
	held.wait("EXISTING_BLOCKED")
	held.close()
	l.runProbe("new-blocked")
	l.runProbe("raw-blocked")
	later := fixture.stats()
	if later.Messages != oldStats.Messages || later.Connections != oldStats.Connections || later.BadSource != 0 {
		t.Fatalf("traffic escaped via underlay after tunnel loss: before=%+v after=%+v", oldStats, later)
	}
	if l.outputDrops() <= drops {
		t.Fatal("no kernel counter evidence for forbidden underlay attempts")
	}
	// The namespace/interface guard also rejects a child that accidentally starts
	// in the parent namespace, rather than quietly creating a host-route dialer.
	expected := l.client.identity
	if _, err = NewWorkerDialer(DialOptions{Expected: expected, Host: l.host, Interface: Device, Resolver: netip.MustParseAddrPort(tunnelServer + ":" + dnsPort)}); !errors.Is(err, ErrScope) {
		t.Fatal("host process constructed a tunnel worker", err)
	}
	fixture.close()
	server.close()
	if !bytes.Equal(before, l.hostSnapshot()) {
		t.Fatal("controlled lab changed host links/routes/firewall/DNS")
	}
	if legacy && !strings.Contains(client.output.text(), "Data Channel: cipher 'AES-128-CBC'") {
		t.Fatal("legacy data cipher was not actually negotiated")
	}
	t.Logf("Observed: legacy-data=%t; mutual TLS handshake with rejected wrong server name; ignored pushed route/DNS; DNS UDP/TCP and business TCP sourced from tunnel; mixed/private DNS rejected; DNS timeout did not fall back; established and new flows blocked after VPN/TUN loss, even with an underlay default route. Counters before=%d after=%d; service before=%+v after=%+v; host network/DNS unchanged.", legacy, drops, l.outputDrops(), oldStats, later)
}
