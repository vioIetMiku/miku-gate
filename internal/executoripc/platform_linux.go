//go:build linux

package executoripc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"mikugate/internal/netexec"
)

type policy struct {
	instance string
	uid, gid uint32
}

func makePolicy(instance string, uid uint32) (policy, error) {
	if _, err := SocketPath(instance); err != nil {
		return policy{}, err
	}
	if uid == 0 || uid >= 1<<31 {
		return policy{}, errors.New("executor requires one explicitly allowed non-root UID")
	}
	account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return policy{}, errors.New("allowed UID must identify an existing local account")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil || gid == 0 || gid >= 1<<31 {
		return policy{}, errors.New("allowed user must have a non-root primary group")
	}
	return policy{instance: instance, uid: uid, gid: uint32(gid)}, nil
}
func peer(conn *net.UnixConn) (*unix.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var credentials *unix.Ucred
	var failure error
	err = raw.Control(func(fd uintptr) {
		credentials, failure = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return nil, failure
	}
	if credentials == nil {
		return nil, ErrPeer
	}
	return credentials, nil
}
func dialTrusted(ctx context.Context, instance string) (net.Conn, error) {
	path, err := SocketPath(instance)
	if err != nil {
		return nil, err
	}
	expected, err := checkSocket(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	raw, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, ErrUnavailable
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		raw.Close()
		return nil, ErrPeer
	}
	credentials, err := peer(conn)
	actual, statErr := checkSocket(path)
	if err != nil || statErr != nil || credentials.Uid != 0 || expected != actual {
		conn.Close()
		return nil, ErrPeer
	}
	return conn, nil
}

// Serve owns one fixed instance and only one explicitly authorized user. Shutting
// down closes IPC, not kernel quarantine resources; inspect recovers them later.
func Serve(ctx context.Context, instance string, uid uint32, ready func(string)) (returned error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("only the executor daemon runs as root; never elevate the web server")
	}
	p, err := makePolicy(instance, uid)
	if err != nil {
		return err
	}
	engine, err := netexec.Open(instance, true)
	if err != nil {
		return err
	}
	defer func() { returned = errors.Join(returned, engine.Close()) }()
	lease, err := openLease(p)
	if err != nil {
		return err
	}
	defer func() { returned = errors.Join(returned, lease.close()) }()
	if ready != nil {
		ready(lease.path)
	}
	return serveListener(ctx, lease.listener, p, newService(instance, engine))
}
func serveListener(ctx context.Context, listener *net.UnixListener, p policy, s *service) error {
	var workers sync.WaitGroup
	defer workers.Wait()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	slots := make(chan struct{}, MaxConnections)
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("executor accept: %w", err)
		}
		credentials, err := peer(conn)
		if err != nil || credentials.Uid != p.uid {
			conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		workers.Add(1)
		go func() { defer workers.Done(); defer func() { <-slots }(); s.handle(ctx, conn) }()
	}
}
