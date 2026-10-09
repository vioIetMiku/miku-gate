//go:build linux

package netexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type tools struct{ ip, nft, nsenter string }
type linuxBackend struct{ tools tools }

// Open creates no listener or persistent process. Mutating sessions take an
// exclusive flock; inspection uses a shared lock and never creates files.
func Open(instanceID string, mutating bool) (*Engine, error) {
	return openRuntime(instanceID, mutating, RuntimeDir)
}
func openRuntime(instanceID string, mutating bool, root string) (*Engine, error) {
	if err := ValidateID(instanceID); err != nil {
		return nil, err
	}
	if os.Geteuid() != 0 {
		return nil, ErrRootRequired
	}
	binaries, err := findTools()
	if err != nil {
		return nil, err
	}
	var header = unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if err = unix.Capget(&header, &caps[0]); err != nil {
		return nil, err
	}
	if caps[0].Effective&(1<<unix.CAP_SYS_ADMIN) == 0 || caps[0].Effective&(1<<unix.CAP_NET_ADMIN) == 0 {
		return nil, errors.New("CAP_SYS_ADMIN and CAP_NET_ADMIN are required for the isolated helper")
	}
	// Match iproute2's mode for the shared namespace registry; never chmod it.
	if mutating {
		if err = secureDirectory("/run", false); err != nil {
			return nil, err
		}
		if err = os.Mkdir("/run/netns", 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	for _, dir := range []string{"/run/netns", root} {
		if err = secureDirectory(dir, mutating); err != nil && !(errors.Is(err, os.ErrNotExist) && !mutating) {
			return nil, err
		}
	}
	sum := sha256.Sum256([]byte(instanceID))
	directory := filepath.Join(root, hex.EncodeToString(sum[:16]))
	if err = secureDirectory(directory, mutating); err != nil && !(errors.Is(err, os.ErrNotExist) && !mutating) {
		return nil, err
	}
	var lock *os.File
	if info, statErr := os.Lstat(directory); statErr == nil {
		if info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("instance runtime directory must be root-only (0700)")
		}
		flags, operation := unix.O_RDONLY, unix.LOCK_SH
		if mutating {
			flags, operation = unix.O_CREAT|unix.O_RDWR, unix.LOCK_EX
		}
		fd, openErr := unix.Open(filepath.Join(directory, ".lock"), flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if openErr != nil {
			return nil, fmt.Errorf("open runtime lock: %w", openErr)
		}
		lock = os.NewFile(uintptr(fd), "runtime-lock")
		if err = secureFile(lock); err != nil {
			lock.Close()
			return nil, err
		}
		if err = unix.Flock(fd, operation|unix.LOCK_NB); err != nil {
			lock.Close()
			return nil, fmt.Errorf("runtime is locked: %w", ErrBusy)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	e, err := newEngine(instanceID, &diskRecords{directory: directory}, &linuxBackend{tools: binaries})
	if err != nil {
		if lock != nil {
			lock.Close()
		}
		return nil, err
	}
	e.readOnly = !mutating
	if lock != nil {
		e.close = func() error { return lock.Close() }
	}
	return e, nil
}

func secureDirectory(path string, create bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("runtime path must be absolute and canonical")
	}
	current := string(filepath.Separator)
	parts := append([]string{""}, strings.Split(strings.TrimPrefix(path, "/"), "/")...)
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe root-owned directory: %s", current)
		}
	}
	return nil
}
func secureFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
		return errors.New("unsafe runtime record or lock: expected root-owned regular 0600 file with one link")
	}
	return nil
}
func trustedBinary(name string) (string, error) {
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		candidate := filepath.Join(dir, name)
		resolved, err := filepath.EvalSymlinks(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err = secureDirectory(filepath.Dir(resolved), false); err != nil {
			return "", err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return "", err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("unsafe executable: %s", resolved)
		}
		return resolved, nil
	}
	return "", fmt.Errorf("required system executable not found in trusted directories: %s", name)
}
func findTools() (tools, error) {
	var t tools
	var err error
	if t.ip, err = trustedBinary("ip"); err != nil {
		return t, err
	}
	if t.nft, err = trustedBinary("nft"); err != nil {
		return t, err
	}
	if t.nsenter, err = trustedBinary("nsenter"); err != nil {
		return t, err
	}
	return t, nil
}

type boundedOutput struct {
	mu sync.Mutex
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Len()+len(p) > 256*1024 {
		b.overflow = true
		return 0, errors.New("command output limit exceeded")
	}
	return b.Buffer.Write(p)
}
func runCommand(ctx context.Context, binary string, args []string, input string, ns *os.File) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = strings.NewReader(input)
	if ns != nil {
		cmd.ExtraFiles = []*os.File{ns}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		err := unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	out := &boundedOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("network command exceeded output budget")
	}
	if err != nil {
		detail := strings.TrimSpace(out.String())
		if len(detail) > 1024 {
			detail = detail[:1024]
		}
		return nil, fmt.Errorf("%s failed: %w (%s)", filepath.Base(binary), err, detail)
	}
	return out.Bytes(), nil
}

func canonical(p Plan) bool {
	expected, err := BuildPlan(p.InstanceID, p.ExitID)
	return err == nil && p.Namespace == expected.Namespace && p.Table == expected.Table && p.Owner == expected.Owner && p.Ruleset == expected.Ruleset
}
func namespaceFile(p Plan) (*os.File, Identity, error) {
	if !canonical(p) {
		return nil, Identity{}, ErrOwnership
	}
	if err := secureDirectory("/run/netns", false); err != nil {
		return nil, Identity{}, err
	}
	fd, err := unix.Open(filepath.Join("/run/netns", p.Namespace), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, Identity{}, err
	}
	file := os.NewFile(uintptr(fd), p.Namespace)
	var stat unix.Stat_t
	var fs unix.Statfs_t
	var host unix.Stat_t
	if err = unix.Fstat(fd, &stat); err == nil {
		err = unix.Fstatfs(fd, &fs)
	}
	if err == nil {
		err = unix.Stat("/proc/self/ns/net", &host)
	}
	id := Identity{Device: uint64(stat.Dev), Inode: stat.Ino}
	if err != nil {
		file.Close()
		return nil, Identity{}, err
	}
	if uint64(fs.Type) != uint64(unix.NSFS_MAGIC) || !id.valid() || (id.Device == uint64(host.Dev) && id.Inode == host.Ino) {
		file.Close()
		return nil, Identity{}, ErrOwnership
	}
	return file, id, nil
}
func ownedNamespace(p Plan, expected Identity) (*os.File, error) {
	f, actual, err := namespaceFile(p)
	if err != nil {
		return nil, err
	}
	if !expected.valid() || actual != expected {
		f.Close()
		return nil, ErrOwnership
	}
	return f, nil
}
func (b *linuxBackend) inNS(ctx context.Context, ns *os.File, binary string, args []string, input string) ([]byte, error) {
	command := append([]string{"--net=/proc/self/fd/3", "--", binary}, args...)
	return runCommand(ctx, b.tools.nsenter, command, input, ns)
}
func (b *linuxBackend) Lookup(ctx context.Context, p Plan) (Identity, bool, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, false, err
	}
	f, id, err := namespaceFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, err
	}
	return id, true, f.Close()
}
func (b *linuxBackend) Create(ctx context.Context, p Plan) (Identity, error) {
	if !canonical(p) {
		return Identity{}, ErrOwnership
	}
	if _, exists, err := b.Lookup(ctx, p); err != nil {
		return Identity{}, err
	} else if exists {
		return Identity{}, ErrOwnership
	}
	if _, err := runCommand(ctx, b.tools.ip, []string{"netns", "add", p.Namespace}, "", nil); err != nil {
		return Identity{}, err
	}
	id, found, err := b.Lookup(ctx, p)
	if err != nil {
		return Identity{}, err
	}
	if !found {
		return Identity{}, ErrOwnership
	}
	return id, nil
}
func (b *linuxBackend) InstallGuard(ctx context.Context, p Plan, id Identity) error {
	ns, err := ownedNamespace(p, id)
	if err != nil {
		return err
	}
	defer ns.Close()
	raw, err := b.inNS(ctx, ns, b.tools.nft, []string{"-j", "list", "ruleset"}, "")
	if err != nil {
		return err
	}
	if !emptyRuleset(raw) {
		return ErrBusy
	}
	_, err = b.inNS(ctx, ns, b.tools.nft, []string{"-f", "-"}, p.Ruleset)
	return err
}
func (b *linuxBackend) Verify(ctx context.Context, p Plan, id Identity) (bool, error) {
	ns, err := ownedNamespace(p, id)
	if err != nil {
		return false, err
	}
	defer ns.Close()
	links, err := b.inNS(ctx, ns, b.tools.ip, []string{"-j", "link", "show"}, "")
	if err != nil || !onlyLoopback(links) {
		return false, err
	}
	raw, err := b.inNS(ctx, ns, b.tools.nft, []string{"-j", "list", "ruleset"}, "")
	return err == nil && guardMatches(raw, p), err
}
func (b *linuxBackend) Delete(ctx context.Context, p Plan, id Identity) error {
	ns, err := ownedNamespace(p, id)
	if err != nil {
		return err
	}
	defer ns.Close()
	links, err := b.inNS(ctx, ns, b.tools.ip, []string{"-j", "link", "show"}, "")
	if err != nil {
		return err
	}
	if !onlyLoopback(links) {
		return ErrBusy
	}
	raw, err := b.inNS(ctx, ns, b.tools.nft, []string{"-j", "list", "ruleset"}, "")
	if err != nil {
		return err
	}
	if !cleanupRulesetSafe(raw, p) {
		return ErrBusy
	}
	pids, err := runCommand(ctx, b.tools.ip, []string{"netns", "pids", p.Namespace}, "", nil)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(pids)) != 0 {
		return ErrBusy
	}
	// The open fd pins the intended namespace for inspection. Recheck the name
	// immediately before deletion; never remove a replacement with another inode.
	current, exists, err := b.Lookup(ctx, p)
	if err != nil {
		return err
	}
	if !exists || current != id {
		return ErrOwnership
	}
	if _, err = runCommand(ctx, b.tools.ip, []string{"netns", "delete", p.Namespace}, "", nil); err != nil {
		return err
	}
	if _, exists, err = b.Lookup(ctx, p); err != nil {
		return err
	} else if exists {
		return ErrOwnership
	}
	return nil
}

var _ io.Writer = (*boundedOutput)(nil)
