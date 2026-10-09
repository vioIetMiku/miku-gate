//go:build linux || windows

package runlock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOneOwnerAndRecoveryWithoutDeletingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := Acquire(path); !errors.Is(err, ErrHeld) {
		if second != nil {
			second.Close()
		}
		t.Fatal("competing owner allowed", err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal("lock not recovered", err)
	}
	second.Close()
	if _, err = os.Stat(path); err != nil {
		t.Fatal("lock file should be retained", err)
	}
}
