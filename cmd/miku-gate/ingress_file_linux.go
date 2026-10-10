//go:build linux

package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path"
	"strings"
)

// Walk with dirfds; do not use a checked name later for a privileged open.
func writePrivateCandidate(destination string, raw []byte) error {
	if !path.IsAbs(destination) || path.Clean(destination) != destination || strings.ContainsAny(destination, "\x00\r\n") {
		return errors.New("候选输出必须是规范绝对新文件路径")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return errors.New("无法打开候选根目录")
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(destination, "/"), "/")
	for _, name := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return errors.New("候选父目录不能含符号链接")
		}
		unix.Close(fd)
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return errors.New("候选父目录必须由当前账号拥有且权限0700")
	}
	filefd, e := unix.Openat(fd, parts[len(parts)-1], unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return errors.New("候选已存在或不能独占创建，未覆盖任何配置")
	}
	f := os.NewFile(uintptr(filefd), destination)
	defer f.Close()
	if e = unix.Fchmod(filefd, 0600); e != nil {
		return errors.New("候选权限设定失败，未应用配置")
	}
	if _, e = f.Write(raw); e != nil {
		return errors.New("候选写入失败，保留文件供检查")
	}
	if e = f.Sync(); e != nil {
		return errors.New("候选同步失败，未应用配置")
	}
	return nil
}
