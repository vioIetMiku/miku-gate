//go:build linux

package tunnel

import (
	"context"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func CurrentNamespace() (NamespaceID, error) {
	var stat unix.Stat_t
	if err := unix.Stat("/proc/self/ns/net", &stat); err != nil {
		return NamespaceID{}, err
	}
	return NamespaceID{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
func workerNetwork(options DialOptions) (func() error, func(context.Context, string, string) (net.Conn, error), error) {
	if options.Expected.Inode == 0 || options.Host.Inode == 0 || options.Expected == options.Host || options.Interface != Device {
		return nil, nil, ErrScope
	}
	current, err := CurrentNamespace()
	if err != nil || current != options.Expected {
		return nil, nil, ErrScope
	}
	iface, err := net.InterfaceByName(options.Interface)
	if err != nil || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
		return nil, nil, ErrScope
	}
	// A newly exec'd process inherits this network namespace for every thread.
	// No thread-local switching, goroutine routing tricks or host resolver fallback.
	verify := func() error {
		current, err := CurrentNamespace()
		if err != nil || current != options.Expected {
			return ErrScope
		}
		actual, err := net.InterfaceByName(options.Interface)
		if err != nil || actual.Index != iface.Index || actual.Flags&net.FlagUp == 0 {
			return ErrScope
		}
		return nil
	}
	control := func(_, _ string, raw syscall.RawConn) error {
		if err := verify(); err != nil {
			return err
		}
		var err error
		if controlErr := raw.Control(func(fd uintptr) {
			err = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, options.Interface)
		}); controlErr != nil {
			return controlErr
		}
		return err
	}
	connect := func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" && network != "udp4" {
			return nil, ErrScope
		}
		if err := verify(); err != nil {
			return nil, err
		}
		d := net.Dialer{Timeout: 2 * time.Second, Control: control}
		conn, err := d.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err = verify(); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
	return verify, connect, nil
}
func namespaceFileID(file *os.File) (NamespaceID, error) {
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if file == nil {
		return NamespaceID{}, ErrScope
	}
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return NamespaceID{}, err
	}
	if err := unix.Fstatfs(int(file.Fd()), &fs); err != nil {
		return NamespaceID{}, err
	}
	current, err := CurrentNamespace()
	id := NamespaceID{Device: uint64(stat.Dev), Inode: stat.Ino}
	if err != nil || uint64(fs.Type) != uint64(unix.NSFS_MAGIC) || id.Inode == 0 || current == id {
		return NamespaceID{}, ErrScope
	}
	return id, nil
}
