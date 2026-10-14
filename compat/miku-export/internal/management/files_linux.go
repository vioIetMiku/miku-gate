//go:build linux

package management

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func readPEM(path string, private bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrHTTPS
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrHTTPS
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOFOLLOW
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, name, flags, 0)
		if e != nil {
			return nil, ErrHTTPS
		}
		unix.Close(fd)
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 && st.Uid != uint32(os.Geteuid()) {
			return nil, ErrHTTPS
		}
		stickyParent := i < len(parts)-1 && st.Uid == 0 && st.Mode&unix.S_ISVTX != 0
		if st.Mode&0022 != 0 && !stickyParent {
			return nil, ErrHTTPS
		}
		if i == len(parts)-1 && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size > 128<<10 || private && st.Mode&0077 != 0) {
			return nil, ErrHTTPS
		}
	}
	f := os.NewFile(uintptr(fd), path)
	fd = -1
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if e != nil || len(raw) > 128<<10 {
		return nil, ErrHTTPS
	}
	return raw, nil
}

func writeBootstrap(destination string, cert, key []byte) error {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || strings.ContainsAny(destination, "\x00\r\n") {
		return ErrHTTPS
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return ErrHTTPS
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(destination, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return ErrHTTPS
		}
		unix.Close(fd)
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return errors.New("自签引导证书父目录必须当前账号拥有且0700，不改现有目录权限")
	}
	name := parts[len(parts)-1]
	if name == "" || unix.Mkdirat(fd, name, 0700) != nil {
		return errors.New("证书目录已存在或无法独占创建，不覆盖证书")
	}
	dir, e := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return ErrHTTPS
	}
	defer unix.Close(dir)
	for name, data := range map[string][]byte{"certificate.pem": cert, "private.key": key} {
		child, e := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if e != nil {
			return ErrHTTPS
		}
		f := os.NewFile(uintptr(child), name)
		_, e = f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return ErrHTTPS
		}
	}
	if unix.Fsync(dir) != nil || unix.Fsync(fd) != nil {
		return ErrHTTPS
	}
	return nil
}
