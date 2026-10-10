//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateFileExclusivePrivateAndNoSymlink(t *testing.T) {
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	file := filepath.Join(parent, "candidate.json")
	if e := writePrivateCandidate(file, []byte("fixture")); e != nil {
		t.Fatal(e)
	}
	if e := writePrivateCandidate(file, []byte("overwrite")); e == nil {
		t.Fatal("candidate overwritten")
	}
	raw, _ := os.ReadFile(file)
	info, _ := os.Stat(file)
	if string(raw) != "fixture" || info.Mode().Perm() != 0600 {
		t.Fatal("candidate privacy/integrity")
	}
	alias := filepath.Join(parent, "alias")
	os.Symlink(parent, alias)
	if e := writePrivateCandidate(filepath.Join(alias, "other"), []byte("x")); e == nil {
		t.Fatal("symlink parent accepted")
	}
	public := filepath.Join(parent, "public")
	os.Mkdir(public, 0755)
	if e := writePrivateCandidate(filepath.Join(public, "other"), []byte("x")); e == nil {
		t.Fatal("nonprivate parent accepted")
	}
}
