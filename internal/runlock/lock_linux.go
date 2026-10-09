//go:build linux

package runlock

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func openFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func validInfo(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}
func lock(f *os.File) error   { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
