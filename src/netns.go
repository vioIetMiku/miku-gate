package main

import (
	"net"
	"os"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// dialerInNetns 返回一个在指定 netns 内建立出站连接的 dial 函数。
// 每次拨号都要切一次 netns，因为 socket 的归属在创建时确定。
func dialerInNetns(nsName string) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)

		go func() {
			runtime.LockOSThread()

			origin, err := os.Open("/proc/self/ns/net")
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer origin.Close()

			target, err := os.Open("/var/run/netns/" + nsName)
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer target.Close()

			if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
				ch <- result{nil, err}
				return
			}

			// 隧道内只有 IPv4 路由。不限定的话 net.Dial 可能选中 AAAA 记录，
			// 那条连接会绕开隧道从母机的 IPv6 出去，暴露真实地址。
			dialer := net.Dialer{Timeout: 30 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
				var bindErr error
				err := raw.Control(func(fd uintptr) {
					bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, "tun0")
				})
				if err != nil {
					return err
				}
				return bindErr
			}}
			conn, dialErr := dialer.Dial(forceIPv4Network(network), addr)

			if err := unix.Setns(int(origin.Fd()), unix.CLONE_NEWNET); err != nil {
				if conn != nil {
					conn.Close()
				}
				ch <- result{nil, err}
				return
			}

			runtime.UnlockOSThread()
			ch <- result{conn, dialErr}
		}()

		r := <-ch
		return r.conn, r.err
	}
}

// forceIPv4Network 把 tcp/udp 收敛成 tcp4/udp4，已经指定版本的原样返回。
func forceIPv4Network(network string) string {
	switch network {
	case "tcp":
		return "tcp4"
	case "udp":
		return "udp4"
	}
	return network
}
