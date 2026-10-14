//go:build !linux

package management

import (
	"errors"
	"os"
)

func readPEM(path string, private bool) ([]byte, error) {
	i, e := os.Lstat(path)
	if e != nil || !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 || i.Size() > 128<<10 {
		return nil, ErrHTTPS
	}
	return os.ReadFile(path)
}
func writeBootstrap(string, []byte, []byte) error {
	return errors.New("IP自签引导证书生成仅支持Linux的私有目录权限，不冒充Windows ACL已校验")
}
