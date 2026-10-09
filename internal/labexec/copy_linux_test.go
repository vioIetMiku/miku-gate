//go:build linux

package labexec

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestConfiguredRootIsFixedAndExplicit(t *testing.T) {
	for _, value := range []string{"", DefaultRoot, PersistentRoot} {
		if _, err := configuredRoot(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"/", "/tmp/miku-gate-lab-exec", PersistentRoot + "/", PersistentRoot + "/..", "/usr/local/lib/other", "relative"} {
		if _, err := configuredRoot(value); err == nil {
			t.Fatal("accepted unexpected lab root", value)
		}
	}
}
func TestNoexecMountIsRefusedNotChanged(t *testing.T) {
	if err := executableMount(unix.ST_NOEXEC | unix.ST_NOSUID | unix.ST_NODEV); err == nil {
		t.Fatal("noexec mount accepted")
	}
	if err := executableMount(unix.ST_NOSUID | unix.ST_NODEV); err != nil {
		t.Fatal(err)
	}
}
func TestCleanupDoesNotDeleteReplacementOrSymlink(t *testing.T) {
	// These refusals also hold without root; no privileged paths are touched.
	root := t.TempDir()
	path := filepath.Join(root, "owned")
	peer := filepath.Join(root, "peer")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peer, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peer, path); err != nil {
		t.Fatal(err)
	}
	if err := removeOwned(path, original); err == nil {
		t.Fatal("replacement symlink accepted")
	}
	if value, err := os.ReadFile(peer); err != nil || string(value) != "keep" {
		t.Fatal("peer changed", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("replacement was removed", err)
	}
}
