//go:build linux

package hostinventory

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Only this fixed metadata file is read; O_NONBLOCK prevents FIFO hangs.
func readSmall(path string, limit int) ([]byte, error) {
	if path != "/etc/os-release" {
		return nil, errors.New("unsupported metadata file")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("invalid metadata file")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(b) > limit {
		return nil, errors.New("metadata bound")
	}
	return b, err
}
func trustedSystemctl() (string, error) {
	path, err := filepath.EvalSymlinks("/usr/bin/systemctl")
	if err != nil {
		return "", err
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("untrusted systemctl path")
		}
		if current == path && !info.Mode().IsRegular() {
			return "", errors.New("invalid systemctl")
		}
		if current == "/" {
			break
		}
	}
	return path, nil
}

type limitedOutput struct {
	mu  sync.Mutex
	raw []byte
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.raw)+len(p) > 16384 {
		return 0, errors.New("service output bound")
	}
	b.raw = append(b.raw, p...)
	return len(p), nil
}
func queryService(ctx context.Context, unit string) ([]byte, error) {
	allowed := false
	for _, s := range specifications {
		for _, u := range s.units {
			if unit == u {
				allowed = true
			}
		}
	}
	if !allowed {
		return nil, errors.New("unsupported service")
	}
	path, err := trustedSystemctl()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--system", "show", "--no-pager", "--property=Id,LoadState,ActiveState,MainPID", "--", unit)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=cat"}
	cmd.Dir = "/"
	cmd.WaitDelay = 200 * time.Millisecond
	output := new(limitedOutput)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		return nil, err
	}
	return output.raw, nil
}
