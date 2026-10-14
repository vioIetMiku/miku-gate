//go:build linux

package secretstore

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func openExisting(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func validKeyInfo(info os.FileInfo) bool {
	m, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && m.Uid == uint32(os.Geteuid()) && m.Nlink == 1 && info.Mode().Perm() == 0600
}
