//go:build linux

package coreruntime

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func coreCommand(options Options, core string, raw []byte, check bool) (*exec.Cmd, error) {
	if os.Geteuid() == 0 {
		return nil, ErrUnavailable
	}
	path := options.XrayPath
	if core == "sing-box" {
		path = options.SingBoxPath
	} else if core != "xray" {
		return nil, ErrUnavailable
	}
	path, err := trustedBinary(path)
	if err != nil {
		return nil, err
	}
	helper, err := trustedBinary("/usr/bin/setpriv")
	if err != nil {
		return nil, err
	}
	args := []string{"run", "-c", "/dev/stdin"}
	if check {
		args = []string{"check", "-c", "/dev/stdin"}
	}
	if core == "xray" {
		args = []string{"run", "-config", "stdin:"}
		if check {
			args = []string{"run", "-test", "-config", "stdin:"}
		}
	}
	cmd := exec.Command(helper, append([]string{"--no-new-privs", "--", path}, args...)...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	cmd.Stdin = bytes.NewReader(raw)
	diagnostics := new(safeCoreLogs)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd, nil
}
func nativeValidator(options Options) Validator {
	return func(ctx context.Context, core string, raw []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cmd, err := coreCommand(options, core, raw, true)
		if err != nil {
			return err
		}
		life, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		process, err := startOwned(life, cmd)
		if err != nil {
			return ErrCore
		}
		select {
		case <-process.done:
			if life.Err() != nil {
				return life.Err()
			}
			if process.waitErr != nil {
				return ErrCore
			}
			return nil
		case <-life.Done():
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := process.Stop(cleanup); err != nil {
				return ErrCore
			}
			return life.Err()
		}
	}
}
