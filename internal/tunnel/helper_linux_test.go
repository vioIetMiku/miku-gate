//go:build linux

package tunnel

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fixtureStats struct{ DNSUDP, DNSTCP, Connections, Messages, BadSource int }
type childFixture struct {
	lab   *vpnLab
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan error
	once  sync.Once
}

func (l *vpnLab) helperCommand(mode string, space labNamespace) *exec.Cmd {
	command := exec.CommandContext(l.ctx, l.nsenter, "--net=/proc/self/fd/3", "--", l.binary, "-test.run=^TestOpenVPNHelper$", "-test.v", "-test.timeout=65s")
	command.ExtraFiles = []*os.File{space.file}
	command.Dir = "/"
	command.WaitDelay = time.Second
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=96MiB", "MIKU_VPN_HELPER=" + mode, fmt.Sprintf("MIKU_VPN_EXPECTED=%d:%d", space.identity.Device, space.identity.Inode), fmt.Sprintf("MIKU_VPN_HOST=%d:%d", l.host.Device, l.host.Inode)}
	return command
}
func (l *vpnLab) startFixture(mode string, space labNamespace) *childFixture {
	l.t.Helper()
	cmd := l.helperCommand(mode, space)
	input, err := cmd.StdinPipe()
	if err != nil {
		l.t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		l.t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	child := &childFixture{lab: l, cmd: cmd, stdin: input, lines: make(chan string, 16), done: make(chan error, 1)}
	if err = cmd.Start(); err != nil {
		l.t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 16384)
		for scanner.Scan() {
			child.lines <- scanner.Text()
		}
		close(child.lines)
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("helper: %w %s", err, stderr.String())
		}
		child.done <- err
	}()
	return child
}
func (c *childFixture) wait(prefix string) string {
	c.lab.t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	var observed []string
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				c.lab.t.Fatalf("helper stopped before %s: %v", prefix, observed)
			}
			observed = append(observed, line)
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-timer.C:
			c.lab.t.Fatalf("helper timeout waiting for %s: %v", prefix, observed)
		case <-c.lab.ctx.Done():
			c.lab.t.Fatal(c.lab.ctx.Err())
		}
	}
}
func (c *childFixture) send(command string) {
	c.lab.t.Helper()
	if _, err := fmt.Fprintln(c.stdin, command); err != nil {
		c.lab.t.Fatal(err)
	}
}
func (c *childFixture) stats() fixtureStats {
	c.send("STATS")
	line := c.wait("STATS ")
	var stats fixtureStats
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "STATS ")), &stats); err != nil {
		c.lab.t.Fatal(err)
	}
	return stats
}
func (c *childFixture) close() {
	c.once.Do(func() {
		c.stdin.Close()
		select {
		case err := <-c.done:
			if err != nil {
				c.lab.t.Error(err)
			}
		case <-time.After(2 * time.Second):
			c.cmd.Process.Kill()
			select {
			case <-c.done:
			case <-time.After(2 * time.Second):
				c.lab.t.Error("test helper did not terminate")
			}
		}
	})
}
func (l *vpnLab) runProbe(mode string) {
	l.t.Helper()
	cmd := l.helperCommand(mode, l.client)
	cmd.Stdin = strings.NewReader("")
	raw, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(raw), "PROBE_OK") {
		l.t.Fatalf("probe %s failed: %v\n%s", mode, err, raw)
	}
}
func parseIdentity(t *testing.T, name string) NamespaceID {
	t.Helper()
	parts := strings.Split(os.Getenv(name), ":")
	if len(parts) != 2 {
		t.Fatal("invalid test namespace identity")
	}
	device, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	inode, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || inode == 0 {
		t.Fatal("invalid inode")
	}
	return NamespaceID{Device: device, Inode: inode}
}
func helperOptions(t *testing.T) DialOptions {
	return DialOptions{Expected: parseIdentity(t, "MIKU_VPN_EXPECTED"), Host: parseIdentity(t, "MIKU_VPN_HOST"), Interface: Device, Resolver: netip.MustParseAddrPort(tunnelServer + ":" + dnsPort)}
}
func helperPolicy(ip string) error {
	if ip == targetIP {
		return nil
	}
	return ErrDestination
}
func echo(conn net.Conn, text string) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(conn, text); err != nil {
		return "", err
	}
	return bufio.NewReader(conn).ReadString('\n')
}
func TestOpenVPNHelper(t *testing.T) {
	mode := os.Getenv("MIKU_VPN_HELPER")
	if mode == "" {
		t.Skip("isolated test subprocess only")
	}
	expected := parseIdentity(t, "MIKU_VPN_EXPECTED")
	current, err := CurrentNamespace()
	if err != nil || current != expected {
		t.Fatal("test worker did not enter expected namespace")
	}
	if mode == "server" {
		runServices(t)
		return
	}
	if mode == "raw-blocked" {
		probeForbiddenUnderlay(t)
		fmt.Println("PROBE_OK")
		return
	}
	options := helperOptions(t)
	d, err := newWorkerDialer(options, helperPolicy)
	if mode == "new-blocked" {
		if !errors.Is(err, ErrScope) {
			t.Fatal("dialer did not fail closed after TUN removal", err)
		}
		fmt.Println("PROBE_OK")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if mode == "held-client" {
		conn, err := d.DialContext(t.Context(), "tcp4", "ok.test:"+targetPort)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err = echo(conn, "before"); err != nil {
			t.Fatal(err)
		}
		fmt.Println("HOLD_READY")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() || scanner.Text() != "CHECK" {
			t.Fatal("missing failure injection signal")
		}
		if _, err = echo(conn, "after"); err == nil {
			t.Fatal("established connection transferred data after tunnel loss")
		}
		fmt.Println("EXISTING_BLOCKED")
		return
	}
	switch mode {
	case "success":
		for _, host := range []string{targetIP, "ok.test", "truncated.test"} {
			conn, err := d.DialContext(t.Context(), "tcp4", host+":"+targetPort)
			if err != nil {
				t.Fatal("dial through tunnel failed", err)
			}
			reply, err := echo(conn, "probe")
			conn.Close()
			if err != nil || !strings.HasPrefix(reply, "FROM 10.243.0.") {
				t.Fatal("unexpected non-tunnel source", reply, err)
			}
		}
	case "policy":
		for _, host := range []string{"blocked.test", "mixed.test", "127.0.0.1", "169.254.169.254"} {
			if conn, err := d.DialContext(t.Context(), "tcp4", host+":"+targetPort); !errors.Is(err, ErrDestination) {
				if conn != nil {
					conn.Close()
				}
				t.Fatal("forbidden resolved destination accepted", host, err)
			}
		}
		public, err := NewWorkerDialer(options)
		if err != nil {
			t.Fatal(err)
		}
		if conn, err := public.DialContext(t.Context(), "tcp4", targetIP+":"+targetPort); err == nil {
			conn.Close()
			t.Fatal("production public policy accepted a laboratory-reserved target")
		}
	case "dns-timeout":
		ctx, cancel := context.WithTimeout(t.Context(), 450*time.Millisecond)
		defer cancel()
		if conn, err := d.DialContext(ctx, "tcp4", "timeout.test:"+targetPort); !errors.Is(err, ErrDNS) {
			if conn != nil {
				conn.Close()
			}
			t.Fatal("DNS failure did not fail closed", err)
		}
		// A working literal connection proves this is DNS failure, not dead topology.
		conn, err := d.DialContext(t.Context(), "tcp4", targetIP+":"+targetPort)
		if err != nil {
			t.Fatal(err)
		}
		_, err = echo(conn, "after-dns-timeout")
		conn.Close()
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("invalid helper mode")
	}
	fmt.Println("PROBE_OK")
}
func probeForbiddenUnderlay(t *testing.T) {
	t.Helper()
	d := net.Dialer{Timeout: 400 * time.Millisecond}
	// Deliberate raw sockets are a negative test only; production Dialer never
	// creates them. Kernel rules must block them after losing the protected TUN.
	if conn, err := d.DialContext(t.Context(), "tcp4", targetIP+":"+targetPort); err == nil {
		conn.Close()
		t.Fatal("raw TCP escaped through the underlay")
	}
	conn, err := d.DialContext(t.Context(), "udp4", transportServer+":"+dnsPort)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(400 * time.Millisecond))
	query := dnsQuery("ok.test")
	if _, err = conn.Write(query); err == nil {
		var response [1024]byte
		if n, err := conn.Read(response[:]); err == nil || n != 0 {
			t.Fatal("raw DNS escaped through the underlay", err)
		}
	}
}
func dnsQuery(name string) []byte {
	raw := make([]byte, 12)
	raw[0], raw[1] = 0x39, 0x39
	raw[2] = 1
	raw[5] = 1
	for _, label := range strings.Split(name, ".") {
		raw = append(raw, byte(len(label)))
		raw = append(raw, label...)
	}
	return append(raw, 0, 0, 1, 0, 1)
}
func answerDNS(raw []byte, tcp bool) ([]byte, bool) {
	if len(raw) < 12 || binary.BigEndian.Uint16(raw[4:6]) != 1 {
		return nil, false
	}
	i := 12
	var labels []string
	for i < len(raw) {
		size := int(raw[i])
		i++
		if size == 0 {
			break
		}
		if size > 63 || i+size > len(raw) {
			return nil, false
		}
		labels = append(labels, string(raw[i:i+size]))
		i += size
	}
	if i+4 > len(raw) || binary.BigEndian.Uint16(raw[i:i+2]) != 1 {
		return nil, false
	}
	i += 4
	name := strings.ToLower(strings.Join(labels, "."))
	if name == "timeout.test" {
		return nil, true
	}
	out := append([]byte(nil), raw[:i]...)
	binary.BigEndian.PutUint16(out[2:4], 0x8180)
	for offset := 6; offset < 12; offset += 2 {
		binary.BigEndian.PutUint16(out[offset:offset+2], 0)
	}
	if name == "truncated.test" && !tcp {
		binary.BigEndian.PutUint16(out[2:4], 0x8380)
		return out, true
	}
	targets := []netip.Addr{netip.MustParseAddr(targetIP)}
	if name == "blocked.test" {
		targets = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	}
	if name == "mixed.test" {
		targets = append(targets, netip.MustParseAddr("127.0.0.1"))
	}
	binary.BigEndian.PutUint16(out[6:8], uint16(len(targets)))
	for _, target := range targets {
		out = append(out, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
		ip := target.As4()
		out = append(out, ip[:]...)
	}
	return out, true
}
func runServices(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	var stats fixtureStats
	observe := func(kind, remote string) {
		mu.Lock()
		defer mu.Unlock()
		host, _, _ := net.SplitHostPort(remote)
		if !strings.HasPrefix(host, "10.243.0.") {
			stats.BadSource++
		}
		switch kind {
		case "udp":
			stats.DNSUDP++
		case "dns-tcp":
			stats.DNSTCP++
		case "tcp":
			stats.Connections++
		case "message":
			stats.Messages++
		}
	}
	udp, err := net.ListenPacket("udp4", "0.0.0.0:"+dnsPort)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	dnsTCP, err := net.Listen("tcp4", "0.0.0.0:"+dnsPort)
	if err != nil {
		t.Fatal(err)
	}
	defer dnsTCP.Close()
	target, err := net.Listen("tcp4", targetIP+":"+targetPort)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		var buffer [4096]byte
		for {
			n, peer, err := udp.ReadFrom(buffer[:])
			if err != nil {
				return
			}
			reply, valid := answerDNS(buffer[:n], false)
			if !valid {
				continue
			}
			observe("udp", peer.String())
			if reply != nil {
				_, _ = udp.WriteTo(reply, peer)
			}
		}
	}()
	go func() {
		for {
			conn, err := dnsTCP.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				var header [2]byte
				if _, err := io.ReadFull(conn, header[:]); err != nil {
					return
				}
				size := int(binary.BigEndian.Uint16(header[:]))
				if size < 12 || size > 4096 {
					return
				}
				raw := make([]byte, size)
				if _, err := io.ReadFull(conn, raw); err != nil {
					return
				}
				reply, valid := answerDNS(raw, true)
				if !valid {
					return
				}
				observe("dns-tcp", conn.RemoteAddr().String())
				if reply == nil {
					return
				}
				binary.BigEndian.PutUint16(header[:], uint16(len(reply)))
				_, _ = conn.Write(append(header[:], reply...))
			}()
		}
	}()
	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			observe("tcp", conn.RemoteAddr().String())
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
				scanner := bufio.NewScanner(conn)
				scanner.Buffer(make([]byte, 128), 1024)
				for scanner.Scan() {
					observe("message", conn.RemoteAddr().String())
					host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
					if _, err := fmt.Fprintf(conn, "FROM %s %s\n", host, scanner.Text()); err != nil {
						return
					}
				}
			}()
		}
	}()
	fmt.Println("SERVICE_READY")
	commands := bufio.NewScanner(os.Stdin)
	for commands.Scan() {
		if commands.Text() != "STATS" {
			t.Fatal("invalid fixture control message")
		}
		mu.Lock()
		snapshot := stats
		mu.Unlock()
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("STATS " + string(raw))
	}
}
