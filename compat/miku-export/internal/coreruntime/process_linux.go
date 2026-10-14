//go:build linux

package coreruntime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"mikugate/internal/ingress"
)

func joinListen(r ingress.ManagedRequest) string {
	return net.JoinHostPort(r.Listen, strconv.Itoa(r.Port))
}
func trustedBinary(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrUnavailable
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", ErrUnavailable
	}
	for current := resolved; ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil {
			return "", ErrUnavailable
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnavailable
		}
		if current == resolved && (!info.Mode().IsRegular() || st.Nlink != 1 || info.Mode().Perm()&0111 == 0) {
			return "", ErrUnavailable
		}
		if current == "/" {
			break
		}
	}
	return resolved, nil
}
func nativeRunner(options Options) Runner {
	return func(ctx context.Context, core string, raw []byte, listen string) (Process, error) {
		// Native core output never reaches a raw logger; info diagnostics are
		// reduced to the closed taxonomy below. Include transport errors that
		// Xray intentionally reports only at info, not warning level.
		var generated map[string]any
		if json.Unmarshal(raw, &generated) != nil {
			return nil, ErrCore
		}
		if log, ok := generated["log"].(map[string]any); ok {
			if core == "xray" {
				log["loglevel"] = "info"
			} else {
				log["level"] = "info"
			}
		}
		raw, e := json.Marshal(generated)
		if e != nil {
			return nil, ErrCore
		}
		if err := nativeValidator(options)(ctx, core, raw); err != nil {
			return nil, err
		}
		host, port, e := net.SplitHostPort(listen)
		if e != nil || net.ParseIP(host) == nil {
			return nil, ErrCore
		}
		n, e := strconv.Atoi(port)
		if e != nil || n < 1024 {
			return nil, ErrCore
		}
		// Preflight refuses port conflicts; ownership verification below handles a race
		// between releasing this reservation and the core actually acquiring the port.
		ln, e := net.Listen("tcp", listen)
		if e != nil {
			return nil, ErrCore
		}
		ln.Close()
		cmd, e := coreCommand(options, core, raw, false)
		if e != nil {
			return nil, e
		}
		process, e := startOwned(ctx, cmd)
		if e != nil {
			return nil, ErrCore
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			if listenerOwned(process.PID(), host, n) && processRestricted(process.PID()) {
				return process, nil
			}
			select {
			case <-process.done:
				return nil, ErrCore
			case <-ctx.Done():
				process.Stop(context.Background())
				return nil, ctx.Err()
			case <-deadline.C:
				process.Stop(context.Background())
				return nil, ErrCore
			case <-ticker.C:
			}
		}
	}
}

type safeCoreLogs struct {
	mu     sync.Mutex
	counts map[string]int
}

func (d *safeCoreLogs) Write(raw []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.counts == nil {
		d.counts = map[string]int{}
	}
	for _, category := range []string{"failed", "invalid", "authentication", "certificate", "timeout", "refused", "REALITY", "verification", "accepted", "TCP", "read", "EOF", "TLS", "dispatch", "Vision", "user", "payload", "splice", "write", "connection reset", "processed invalid", "account", "flow", "forward", "closing", "failed to read client hello", "authentication failed or validation criteria not met", "target sent incorrect server hello or handshake incomplete", "handshake did not complete successfully", "server name mismatch", "unsupported TLS version", "failed to dial dest", "payload[0]"} {
		if strings.Contains(string(raw), category) {
			d.counts[category]++
		}
	}
	return len(raw), nil
}
func (d *safeCoreLogs) Summary() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	labels := make([]string, 0, len(d.counts))
	for label, count := range d.counts {
		labels = append(labels, label+"="+strconv.Itoa(count))
	}
	sort.Strings(labels)
	return labels
}

type nativeProcess struct {
	diagnostics *safeCoreLogs
	cmd         *exec.Cmd
	done        chan struct{}
	waitErr     error
	once        sync.Once
}

func startOwned(ctx context.Context, cmd *exec.Cmd) (*nativeProcess, error) {
	p := &nativeProcess{cmd: cmd, done: make(chan struct{})}
	if diagnostics, ok := cmd.Stderr.(*safeCoreLogs); ok {
		p.diagnostics = diagnostics
	}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if ctx.Err() != nil {
			ready <- ctx.Err()
			close(p.done)
			return
		}
		if e := cmd.Start(); e != nil {
			ready <- e
			close(p.done)
			return
		}
		ready <- nil
		stop := context.AfterFunc(ctx, func() { p.once.Do(func() { cmd.Process.Kill() }) })
		defer stop()
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	if e := <-ready; e != nil {
		return nil, e
	}
	return p, nil
}
func (p *nativeProcess) Done() <-chan struct{} { return p.done }
func (p *nativeProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *nativeProcess) Stop(ctx context.Context) error {
	select {
	case <-p.done:
		return nil
	default:
	}
	p.once.Do(func() { p.cmd.Process.Kill() })
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case <-p.done:
		return nil
	case <-bounded.Done():
		return errors.New("owned core stop timeout")
	}
}
func listenerOwned(pid int, host string, port int) bool {
	if pid < 1 {
		return false
	}
	inodes := map[string]bool{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, e := os.ReadFile(file)
		if e != nil || len(raw) > 1<<20 {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			a, p, ok := strings.Cut(f[1], ":")
			n, e := strconv.ParseUint(p, 16, 16)
			ipRaw, ipErr := hex.DecodeString(a)
			if ipErr != nil || (len(ipRaw) != 4 && len(ipRaw) != 16) {
				continue
			}
			for start := 0; start < len(ipRaw); start += 4 {
				ipRaw[start], ipRaw[start+3] = ipRaw[start+3], ipRaw[start]
				ipRaw[start+1], ipRaw[start+2] = ipRaw[start+2], ipRaw[start+1]
			}
			if ok && e == nil && int(n) == port && net.ParseIP(host).Equal(net.IP(ipRaw)) {
				inodes[f[9]] = true
			}
		}
	}
	// FD ownership, not a TCP connect to someone else's coincident listener.
	entries, e := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/fd")
	if e != nil || len(entries) > 256 {
		return false
	}
	for _, entry := range entries {
		link, e := os.Readlink("/proc/" + strconv.Itoa(pid) + "/fd/" + entry.Name())
		if e == nil && strings.HasPrefix(link, "socket:[") && inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
			return true
		}
	}
	return false
}

func processRestricted(pid int) bool {
	raw, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if e != nil || len(raw) > 16384 {
		return false
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if fields["NoNewPrivs"] != "1" || fields["CapEff"] != "0000000000000000" {
		return false
	}
	ids := strings.Fields(fields["Uid"])
	if len(ids) != 4 {
		return false
	}
	expected := strconv.Itoa(os.Geteuid())
	for _, id := range ids {
		if id != expected || id == "0" {
			return false
		}
	}
	return true
}

func (p *nativeProcess) DiagnosticCategories() []string {
	if p.diagnostics == nil {
		return nil
	}
	return p.diagnostics.Summary()
}
