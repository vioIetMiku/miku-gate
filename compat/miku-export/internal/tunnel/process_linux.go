//go:build linux

package tunnel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// process belongs only to the controlled lab at this stage. No CLI or IPC method
// exposes it, and the full gateway still cannot start tunnels from cached nodes.
type process struct {
	cmd         *exec.Cmd
	done, ready chan struct{}
	mu          sync.Mutex
	err         error
	output      *processOutput
	once        sync.Once
}
type processOutput struct {
	mu    sync.Mutex
	tail  []byte
	ready chan struct{}
	once  sync.Once
}

func (o *processOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	combined := append(o.tail, data...)
	if bytes.Contains(combined, []byte("Initialization Sequence Completed")) {
		o.once.Do(func() { close(o.ready) })
	}
	if len(combined) > 16384 {
		combined = append([]byte(nil), combined[len(combined)-16384:]...)
	}
	o.tail = combined
	return len(data), nil
}
func (o *processOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return string(o.tail) }
func trustedTool(name string) (string, error) {
	for _, directory := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		full, err := filepath.EvalSymlinks(filepath.Join(directory, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		path := full
		for {
			info, err := os.Lstat(path)
			if err != nil {
				return "", err
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
				return "", errors.New("unsafe system executable path")
			}
			if path == full && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
				return "", errors.New("unsafe system executable")
			}
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
		return full, nil
	}
	return "", fmt.Errorf("required system tool unavailable: %s", name)
}
func sealedConfig(text string) (*os.File, error) {
	if len(text) == 0 || len(text) > MaxProfileBytes {
		return nil, ErrProfile
	}
	fd, err := unix.MemfdCreate("miku-openvpn-config", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "openvpn-config")
	failed := true
	defer func() {
		if failed {
			file.Close()
		}
	}()
	if err = unix.Fchmod(fd, 0600); err != nil {
		return nil, err
	}
	if _, err = file.WriteString(text); err != nil {
		return nil, err
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	if _, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	failed = false
	return file, nil
}
func startProcess(ctx context.Context, namespace *os.File, config string) (*process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := namespaceFileID(namespace); err != nil {
		return nil, err
	}
	openvpn, err := trustedTool("openvpn")
	if err != nil {
		return nil, err
	}
	nsenter, err := trustedTool("nsenter")
	if err != nil {
		return nil, err
	}
	file, err := sealedConfig(config)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	cmd := exec.Command(nsenter, "--net=/proc/self/fd/3", "--", openvpn, "--config", "/proc/self/fd/4")
	cmd.ExtraFiles = []*os.File{namespace, file}
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	p := &process{cmd: cmd, done: make(chan struct{}), ready: make(chan struct{})}
	p.output = &processOutput{ready: p.ready}
	cmd.Stdout = p.output
	cmd.Stderr = p.output
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	go func() { err := cmd.Wait(); p.mu.Lock(); p.err = err; p.mu.Unlock(); close(p.done) }()
	go func() {
		select {
		case <-ctx.Done():
			p.close()
		case <-p.done:
		}
	}()
	return p, nil
}
func (p *process) waitReady(ctx context.Context) error {
	select {
	case <-p.ready:
		return nil
	case <-p.done:
		return errors.New("OpenVPN exited before initialization")
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *process) close() {
	p.once.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
			return
		case <-time.After(2 * time.Second):
		}
		_ = unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
		}
	})
}
func startClient(ctx context.Context, namespace *os.File, profile Profile) (*process, error) {
	if !profile.endpoint.IP.Is4() || profile.endpoint.Port == 0 || profile.ca == "" || !strings.Contains(profile.render(), "script-security 1\n") {
		return nil, ErrProfile
	}
	return startProcess(ctx, namespace, profile.render())
}
