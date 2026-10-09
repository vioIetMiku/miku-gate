// Package runlock ensures one control-service process owns a data directory.
// The file is deliberately retained; only the OS lock denotes a live owner.
package runlock

import (
	"errors"
	"os"
	"sync"
)

var ErrHeld = errors.New("该数据目录已有控制服务运行，或控制锁不安全；不会启动第二个配置写入者")

type Lease struct {
	file *os.File
	once sync.Once
}

func Acquire(path string) (*Lease, error) {
	original, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, ErrHeld
	}
	if original != nil && (!original.Mode().IsRegular() || original.Mode()&os.ModeSymlink != 0) {
		return nil, ErrHeld
	}
	file, err := openFile(path)
	if err != nil {
		return nil, ErrHeld
	}
	info, err := file.Stat()
	if err != nil || !validInfo(info) || (original != nil && !os.SameFile(original, info)) {
		file.Close()
		return nil, ErrHeld
	}
	if err = lock(file); err != nil {
		file.Close()
		return nil, ErrHeld
	}
	return &Lease{file: file}, nil
}
func (l *Lease) Close() error {
	var err error
	l.once.Do(func() { err = errors.Join(unlock(l.file), l.file.Close()) })
	return err
}
