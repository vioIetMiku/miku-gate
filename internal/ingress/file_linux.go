//go:build linux

package ingress

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
)

// Only known absolute paths. Open each directory with O_NOFOLLOW; root ownership
// and no group/world write are checked on opened descriptors, not just names.
func ReadSource(source Source) ([]byte, error) {
	known, e := FindSource(source.ID)
	if e != nil || known != source {
		return nil, errors.New("固定配置来源不匹配")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, errors.New("无法打开配置目录")
	}
	defer func() { unix.Close(fd) }()
	segments := strings.Split(strings.TrimPrefix(source.Path, "/"), "/")
	for i, name := range segments {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if i < len(segments)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, name, flags, 0)
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				return nil, errors.New("常见位置未找到配置")
			}
			return nil, errors.New("配置不可读或含符号链接；需授权安装助手检测，不放宽权限")
		}
		unix.Close(fd)
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			return nil, errors.New("配置路径归属/权限不满足安全读取要求")
		}
		if i == len(segments)-1 && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size > MaxConfig) {
			return nil, errors.New("配置不是有界独立普通文件")
		}
	}
	f := os.NewFile(uintptr(fd), source.Path)
	fd = -1
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, MaxConfig+1))
	if e != nil || len(raw) > MaxConfig {
		return nil, errors.New("读取配置失败或超出上限")
	}
	return raw, nil
}
