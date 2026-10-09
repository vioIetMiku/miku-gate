//go:build linux

package executoripc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type fileID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type socketRecord struct {
	Version  int    `json:"version"`
	Instance string `json:"instance_id"`
	UID      uint32 `json:"allowed_uid"`
	GID      uint32 `json:"allowed_gid"`
	Identity fileID `json:"identity"`
}
type socketLease struct {
	listener *net.UnixListener
	lock     *os.File
	path     string
	record   socketRecord
}

func checkDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrPeer
	}
	current := path
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return ErrPeer
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}
func createDirectory(path string, mode os.FileMode) error {
	if err := checkDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return checkDirectory(path)
}
func checkSocket(path string) (fileID, error) {
	if err := checkDirectory(filepath.Dir(path)); err != nil {
		return fileID{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fileID{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || stat.Uid != 0 || info.Mode().Perm() != 0660 {
		return fileID{}, ErrPeer
	}
	return fileID{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
func secureFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
		return ErrPeer
	}
	return nil
}
func loadRecord(path string) (socketRecord, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return socketRecord{}, false, nil
	}
	if err != nil {
		return socketRecord{}, false, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if err = secureFile(file); err != nil {
		return socketRecord{}, true, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return socketRecord{}, true, err
	}
	var record socketRecord
	if len(raw) > 4096 || decode(raw, &record) != nil {
		return record, true, ErrPeer
	}
	return record, true, nil
}
func saveRecord(path string, record socketRecord) error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".socket-"+hex.EncodeToString(nonce[:]))
	fd, err := unix.Open(tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmp)
	defer func() { file.Close(); _ = os.Remove(tmp) }()
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func openLease(p policy) (*socketLease, error) {
	path, err := SocketPath(p.instance)
	if err != nil {
		return nil, err
	}
	if err = createDirectory(SocketRoot, 0755); err != nil {
		return nil, err
	}
	directory := filepath.Dir(path)
	if err = createDirectory(directory, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(directory, ".lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), "ipc-lock")
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	if err = secureFile(lock); err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.New("executor IPC socket is locked")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() != ".lock" && entry.Name() != "control.sock" && entry.Name() != "socket.json" {
			return nil, errors.New("unexpected IPC runtime object; refusing automatic cleanup")
		}
	}
	recordPath := filepath.Join(directory, "socket.json")
	old, recorded, err := loadRecord(recordPath)
	if err != nil {
		return nil, err
	}
	if recorded && (old.Version != Version || old.Instance != p.instance || old.UID != p.uid || old.GID != p.gid || old.Identity.Inode == 0) {
		return nil, ErrPeer
	}
	actual, socketErr := checkSocket(path)
	if socketErr == nil {
		if !recorded || actual != old.Identity {
			return nil, ErrPeer
		}
		// The namespace lock alone cannot prove this socket is dead. Never unlink a
		// live or replaced endpoint, even if it has a familiar deterministic name.
		conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("existing executor socket is still live")
		}
		if !errors.Is(dialErr, unix.ECONNREFUSED) {
			return nil, ErrPeer
		}
		recheck, err := checkSocket(path)
		if err != nil || recheck != old.Identity {
			return nil, ErrPeer
		}
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(socketErr, os.ErrNotExist) {
		return nil, socketErr
	}
	if recorded {
		if err = os.Remove(recordPath); err != nil {
			return nil, err
		}
	}
	// This directory is root-owned and contains only the checked lease objects.
	// Keep it root-only while the new socket is bound and permissions are set.
	if err = os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	created, err := os.Lstat(path)
	if err != nil {
		listener.Close()
		return nil, err
	}
	stat, ok := created.Sys().(*syscall.Stat_t)
	if !ok {
		listener.Close()
		return nil, ErrPeer
	}
	identity := fileID{Device: uint64(stat.Dev), Inode: stat.Ino}
	established := false
	defer func() {
		if established {
			return
		}
		listener.Close()
		// No recursive removal, and no deletion if the socket was replaced.
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		now, ok := info.Sys().(*syscall.Stat_t)
		if ok && info.Mode()&os.ModeSocket != 0 && uint64(now.Dev) == identity.Device && now.Ino == identity.Inode {
			_ = os.Remove(path)
		}
	}()
	if err = os.Chown(path, 0, int(p.gid)); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0660); err != nil {
		return nil, err
	}
	record := socketRecord{Version: Version, Instance: p.instance, UID: p.uid, GID: p.gid, Identity: identity}
	if err = saveRecord(recordPath, record); err != nil {
		return nil, err
	}
	if err = os.Chown(directory, 0, int(p.gid)); err != nil {
		return nil, err
	}
	if err = os.Chmod(directory, 0750); err != nil {
		return nil, err
	}
	established, success = true, true
	return &socketLease{listener: listener, lock: lock, path: path, record: record}, nil
}
func (l *socketLease) close() error {
	_ = l.listener.Close()
	defer l.lock.Close()
	actual, err := checkSocket(l.path)
	if err != nil {
		return err
	}
	if actual != l.record.Identity {
		return fmt.Errorf("IPC socket replaced; %w", ErrPeer)
	}
	recordPath := filepath.Join(filepath.Dir(l.path), "socket.json")
	record, found, err := loadRecord(recordPath)
	if err != nil || !found || record != l.record {
		return ErrPeer
	}
	if err = os.Remove(l.path); err != nil {
		return err
	}
	if err = os.Remove(recordPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(l.path))
}
