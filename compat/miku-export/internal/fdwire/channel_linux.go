//go:build linux

// Package fdwire moves bounded messages and at most one stream descriptor over a
// private inherited Unix sequenced-packet channel. It does not expose a listener.
package fdwire

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"mikugate/internal/wirejson"
)

const MaxMessage = 2048

var ErrMessage = errors.New("invalid private descriptor message")

type Channel struct {
	Conn *net.UnixConn
	mu   sync.Mutex
}

func Pair(packet bool) (*os.File, *os.File, error) {
	kind := unix.SOCK_STREAM
	if packet {
		kind = unix.SOCK_SEQPACKET
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, kind|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(pair[0]), "private-channel"), os.NewFile(uintptr(pair[1]), "private-channel"), nil
}
func FromFile(file *os.File) (*Channel, error) {
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	u, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, ErrMessage
	}
	if u.LocalAddr().Network() != "unixpacket" {
		u.Close()
		return nil, ErrMessage
	}
	return &Channel{Conn: u}, nil
}
func (c *Channel) Send(message any, file *os.File) error {
	raw, err := json.Marshal(message)
	if err != nil || len(raw) > MaxMessage {
		return ErrMessage
	}
	var rights []byte
	if file != nil {
		rights = unix.UnixRights(int(file.Fd()))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	n, oob, err := c.Conn.WriteMsgUnix(raw, rights, nil)
	if err != nil {
		return err
	}
	if n != len(raw) || oob != len(rights) {
		return ErrMessage
	}
	return nil
}
func (c *Channel) Receive(message any) (*os.File, error) {
	raw := make([]byte, MaxMessage+1)
	ancillary := make([]byte, unix.CmsgSpace(4*8))
	var n, oob, flags int
	rawConn, err := c.Conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var receiveErr error
	err = rawConn.Read(func(fd uintptr) bool {
		n, oob, flags, _, receiveErr = unix.Recvmsg(int(fd), raw, ancillary, unix.MSG_CMSG_CLOEXEC)
		return receiveErr != unix.EAGAIN && receiveErr != unix.EWOULDBLOCK
	})
	if err == nil {
		err = receiveErr
	}
	if err != nil {
		return nil, err
	}
	messages, parseErr := unix.ParseSocketControlMessage(ancillary[:oob])
	fds := []int{}
	invalid := parseErr != nil
	for _, item := range messages {
		if item.Header.Level != unix.SOL_SOCKET || item.Header.Type != unix.SCM_RIGHTS {
			invalid = true
			continue
		}
		rights, e := unix.ParseUnixRights(&item)
		if e != nil {
			invalid = true
			continue
		}
		fds = append(fds, rights...)
	}
	// MSG_CMSG_CLOEXEC makes receipt atomic against concurrent process creation.
	for _, fd := range fds {
		unix.CloseOnExec(fd)
	}
	if invalid || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > MaxMessage || len(fds) > 1 || wirejson.Decode(raw[:n], message, MaxMessage) != nil {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return nil, ErrMessage
	}
	if len(fds) == 0 {
		return nil, nil
	}
	fd := fds[0]
	kind, e := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	address, aerr := unix.Getsockname(fd)
	if _, ok := address.(*unix.SockaddrUnix); !ok || aerr != nil || e != nil || kind != unix.SOCK_STREAM {
		unix.Close(fd)
		return nil, ErrMessage
	}
	return os.NewFile(uintptr(fd), "connected-stream"), nil
}
func (c *Channel) Close() error { return c.Conn.Close() }
