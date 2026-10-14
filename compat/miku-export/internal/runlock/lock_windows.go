//go:build windows

package runlock

import (
	"golang.org/x/sys/windows"
	"os"
)

func openFile(path string) (*os.File, error) { return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600) }
func validInfo(info os.FileInfo) bool        { return info.Mode().IsRegular() }
func lock(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
