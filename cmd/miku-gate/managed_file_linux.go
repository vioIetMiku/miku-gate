//go:build linux

package main

import (
	"errors"
	"os"
	"path"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

func writeManagedDirectory(destination string, files map[string][]byte) error {
	if !path.IsAbs(destination) || path.Clean(destination) != destination || strings.ContainsAny(destination, "\x00\r\n") {
		return errors.New("bundle需规范绝对路径")
	}
	names := []string{}
	for name, raw := range files {
		switch name {
		case "server.json", "client.json", "client-uri.txt", "BUNDLE.json", "certificate.pem", "private.key":
		default:
			return errors.New("未知bundle文件")
		}
		if len(raw) > 2<<20 {
			return errors.New("bundle文件超出限制")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return errors.New("bundle目录无法打开")
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(destination, "/"), "/")
	for _, name := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if e != nil {
			return errors.New("bundle父目录不允许符号链接")
		}
		unix.Close(fd)
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return errors.New("bundle父目录必须由当前账号拥有且0700")
	}
	leaf := parts[len(parts)-1]
	if e = unix.Mkdirat(fd, leaf, 0700); e != nil {
		return errors.New("bundle目录已存在或无法独占创建，不覆盖")
	}
	dir, e := unix.Openat(fd, leaf, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return errors.New("新bundle无法打开，未应用，保留目录供检查")
	}
	defer unix.Close(dir)
	if e = unix.Fchmod(dir, 0700); e != nil {
		return errors.New("bundle权限无法设置，未应用")
	}
	for _, name := range names {
		filefd, e := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if e != nil {
			return errors.New("bundle写入未完成；保留私有目录，不应用配置")
		}
		f := os.NewFile(uintptr(filefd), name)
		_, e = f.Write(files[name])
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return errors.New("bundle同步失败，未应用配置")
		}
	}
	if unix.Fsync(dir) != nil || unix.Fsync(fd) != nil {
		return errors.New("bundle目录同步失败，未应用")
	}
	return nil
}
