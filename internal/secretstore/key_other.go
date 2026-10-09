//go:build !linux

package secretstore

import "os"

// Non-Linux builds are developer clients; OS ACLs must protect the data directory.
func openExisting(path string) (*os.File, error) { return os.Open(path) }
func validKeyInfo(info os.FileInfo) bool         { return info.Mode().IsRegular() }
