//go:build linux

package secretstore

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyObjectAndPermissionSafety(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "hardlink", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			key := filepath.Join(dir, "key")
			peer := filepath.Join(dir, "peer")
			if err := os.WriteFile(peer, make([]byte, 32), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(peer, key); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(key, 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(peer, key); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.WriteFile(key, make([]byte, 32), 0644); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() { _, err := Open(key, true); done <- err }()
			select {
			case err := <-done:
				if !errors.Is(err, ErrKey) {
					t.Fatal("unsafe key accepted", err)
				}
			case <-time.After(time.Second):
				t.Fatal("unsafe key blocked the reader")
			}
			if raw, err := os.ReadFile(peer); err != nil || len(raw) != 32 {
				t.Fatal("peer changed", err)
			}
		})
	}
}
