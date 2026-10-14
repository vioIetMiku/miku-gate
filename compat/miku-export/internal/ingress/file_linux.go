//go:build linux

package ingress

import (
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Only known absolute paths. Open each directory with O_NOFOLLOW; root ownership
// and no group/world write are checked on opened descriptors, not just names.
func ReadSource(source Source) ([]byte, error) { return readSource(source, 0) }

// Inspect only: an administrator may explicitly acknowledge the installer UID
// on the two fixed panel directories. This is not permission to execute any
// binary there or to apply/migrate the original configuration.
func ReadPanelSource(source Source, owner uint32) ([]byte, error) {
	if !source.PanelManaged || source.ID != "x-ui" || owner == 0 {
		return nil, errors.New("安装目录所有者例外只允许明确的 x-ui 只读检查")
	}
	return readSource(source, owner)
}
func readSource(source Source, allowedOwner uint32) ([]byte, error) {
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
		if unix.Fstat(fd, &st) != nil || st.Mode&0022 != 0 || !directoryOwnerAllowed(source, i, len(segments), st.Uid, allowedOwner) {
			return nil, errors.New("配置路径归属/权限不满足安全读取要求；面板目录非root所有时仅可显式确认所有者做inspect，不授权应用")
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

// indices 2 and 3 are exactly /usr/local/x-ui and its bin directory.
func directoryOwnerAllowed(source Source, index, total int, uid, allowed uint32) bool {
	if uid == 0 {
		return true
	}
	return source.ID == "x-ui" && source.PanelManaged && allowed != 0 && uid == allowed && index < total-1 && (index == 2 || index == 3)
}

func ObserveListeners(report *Report) {
	var all []socket
	complete := true
	for _, item := range []struct {
		path string
		ipv6 bool
	}{{"/proc/net/tcp", false}, {"/proc/net/tcp6", true}} {
		fd, err := unix.Open(item.path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			complete = false
			continue
		}
		f := os.NewFile(uintptr(fd), item.path)
		raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if err != nil {
			complete = false
			continue
		}
		sockets, valid := parseListeners(raw, item.ipv6)
		if !valid {
			complete = false
			continue
		}
		all = append(all, sockets...)
	}
	correlate(report.Inbounds, all, complete)
	report.Warnings = append(report.Warnings, "listener仅关联当前网络空间的TCP监听地址/端口；匹配不证明由所选核心/PID持有，IPv6双栈与socket激活可能无法精确匹配。尚未核验进程归属或客户端可达。")
}
