//go:build linux

// Package labexec contains opt-in Linux test support, not production launchers.
// Only integration _test.go files import it. Executables are kept separate from
// runtime state so a host's /run noexec policy does not need to be weakened.
package labexec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const RootEnvironment = "MIKU_GATE_LAB_EXEC_ROOT"
const DefaultRoot = "/run/miku-gate-lab-exec"
const PersistentRoot = "/usr/local/lib/miku-gate-lab-exec"
const maxExecutableBytes = 64 << 20

func configuredRoot(value string) (string, error) {
	switch value {
	case "", DefaultRoot:
		return DefaultRoot, nil
	case PersistentRoot:
		return PersistentRoot, nil
	default:
		return "", errors.New("lab executable root must be one of the two documented fixed paths")
	}
}

func executableMount(flags int64) error {
	if flags&unix.ST_NOEXEC != 0 {
		return fmt.Errorf("lab executable filesystem is noexec; explicitly use %s=%s on an executable filesystem, never remount /run", RootEnvironment, PersistentRoot)
	}
	return nil
}

func trustedAncestors(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		metadata, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || metadata.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0001 == 0 {
			return fmt.Errorf("unsafe or non-traversable lab executable ancestor: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func prepareRoot(root string) error {
	parent := filepath.Dir(root)
	if err := trustedAncestors(parent); err != nil {
		return err
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(parent, &filesystem); err != nil {
		return err
	}
	if err := executableMount(int64(filesystem.Flags)); err != nil {
		return err
	}
	err := os.Mkdir(root, 0755)
	created := err == nil
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	directory, err := openDirectory(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || metadata.Uid != 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe lab executable root")
	}
	// Only a directory created by this call may have its umask-adjusted mode set.
	// Never take over or chmod an existing directory with unexpected permissions.
	if created {
		if err := directory.Chmod(0755); err != nil {
			return err
		}
	} else if info.Mode().Perm() != 0755 {
		return errors.New("existing lab executable root must already be root-owned 0755")
	}
	if err := unix.Fstatfs(int(directory.Fd()), &filesystem); err != nil {
		return err
	}
	return executableMount(int64(filesystem.Flags))
}

func removeOwned(path string, original os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	metadata, ok := current.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != 0 || !os.SameFile(original, current) || current.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("lab cleanup refuses replaced object: %s", path)
	}
	if current.Mode().IsRegular() && metadata.Nlink != 1 {
		return fmt.Errorf("lab cleanup refuses multiply linked executable: %s", path)
	}
	// Remove is non-recursive: an unexpected child retains the directory.
	return os.Remove(path)
}

// CopySelf creates one root-owned, read-only executable copy for this test.
// An ordinary test UID can traverse/read/execute it but cannot replace it.
// Cleanup checks identities and never recursively deletes a directory.
func CopySelf(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 || (os.Getenv("MIKU_GATE_NETNS_TEST") != "1" && os.Getenv("MIKU_GATE_OPENVPN_TEST") != "1") {
		t.Fatal("lab executable copies require explicit root integration-test opt-in")
	}
	root, err := configuredRoot(os.Getenv(RootEnvironment))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRoot(root); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "exec-")
	if err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := removeOwned(directory, directoryInfo); err != nil {
			t.Error(err)
		}
	})
	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	sourceInfo, err := source.Stat()
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() > maxExecutableBytes {
		t.Fatal("invalid test executable source", err)
	}
	path := filepath.Join(directory, "helper.test")
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := target.Stat()
	if err != nil {
		target.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := removeOwned(path, targetInfo); err != nil {
			t.Error(err)
		}
	})
	copied, copyErr := io.Copy(target, io.LimitReader(source, maxExecutableBytes+1))
	if copyErr != nil || copied != sourceInfo.Size() {
		target.Close()
		t.Fatal("test executable copy failed or source changed", copyErr)
	}
	if err := target.Chmod(0555); err != nil {
		target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	// The private staging directory is exposed only after the complete binary is
	// read-only and its writable descriptor has closed (avoids ETXTBSY).
	dirFD, err := openDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := dirFD.Chmod(0755); err != nil {
		dirFD.Close()
		t.Fatal(err)
	}
	if err := dirFD.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Lab helper executable: root-owned 0555 under %s; runtime paths and mount policies unchanged.", root)
	return path
}
