//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedBundlePrivateExclusiveAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	dir := filepath.Join(root, "new")
	files := map[string][]byte{"server.json": []byte("secret"), "client.json": []byte("client")}
	if e := writeManagedDirectory(dir, files); e != nil {
		t.Fatal(e)
	}
	if e := writeManagedDirectory(dir, files); e == nil {
		t.Fatal("existing bundle replaced")
	}
	i, _ := os.Stat(dir)
	f, _ := os.Stat(filepath.Join(dir, "server.json"))
	if i.Mode().Perm() != 0700 || f.Mode().Perm() != 0600 {
		t.Fatal("wrong privacy")
	}
	if e := writeManagedDirectory(filepath.Join(root, "bad"), map[string][]byte{"../../shadow": []byte("x")}); e == nil {
		t.Fatal("arbitrary path")
	}
	alias := filepath.Join(root, "alias")
	os.Symlink(root, alias)
	if e := writeManagedDirectory(filepath.Join(alias, "bad"), files); e == nil {
		t.Fatal("symlink parent")
	}
}
