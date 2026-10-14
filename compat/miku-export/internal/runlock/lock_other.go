//go:build !linux && !windows

package runlock

import (
	"errors"
	"os"
)

func openFile(string) (*os.File, error) {
	return nil, errors.New("control runtime unsupported on this platform")
}
func validInfo(os.FileInfo) bool { return false }
func lock(*os.File) error        { return ErrHeld }
func unlock(*os.File) error      { return nil }
