//go:build linux

package fdwire

import (
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func channels(t *testing.T) (*Channel, *Channel) {
	t.Helper()
	a, b, err := Pair(true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	left, err := FromFile(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := FromFile(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { left.Close(); right.Close() })
	left.Conn.SetDeadline(time.Now().Add(time.Second))
	right.Conn.SetDeadline(time.Now().Add(time.Second))
	return left, right
}
func TestDescriptorRoundTripAndCloexec(t *testing.T) {
	left, right := channels(t)
	a, b, err := Pair(false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	message := struct {
		Operation string `json:"operation"`
	}{"dial"}
	if left.Send(message, b) != nil {
		t.Fatal("send")
	}
	received, err := right.Receive(&message)
	if err != nil || received == nil {
		t.Fatal(err)
	}
	defer received.Close()
	flags, err := unix.FcntlInt(received.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("received descriptor can leak across exec")
	}
	conn, err := net.FileConn(received)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go a.Write([]byte("x"))
	var data [1]byte
	if _, err = io.ReadFull(conn, data[:]); err != nil || data[0] != 'x' {
		t.Fatal("wrong descriptor", err)
	}
}
func TestRejectsFilesAndMultipleRights(t *testing.T) {
	for _, kind := range []string{"file", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			left, right := channels(t)
			file, err := os.Create(filepath.Join(t.TempDir(), "ordinary"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			rights := unix.UnixRights(int(file.Fd()))
			if kind == "multiple" {
				rights = unix.UnixRights(int(file.Fd()), int(file.Fd()))
			}
			if _, _, err = left.Conn.WriteMsgUnix([]byte(`{"operation":"dial"}`), rights, nil); err != nil {
				t.Fatal(err)
			}
			var message struct {
				Operation string `json:"operation"`
			}
			if fd, err := right.Receive(&message); err == nil || fd != nil {
				if fd != nil {
					fd.Close()
				}
				t.Fatal("invalid rights accepted", err)
			}
			if _, err = file.Write([]byte("still owned")); err != nil {
				t.Fatal("sender descriptor was closed", err)
			}
		})
	}
}
