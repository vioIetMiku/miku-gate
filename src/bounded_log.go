package main

import (
	"os"
	"sync"
)

const maxCoreLogBytes = 2 << 20

// Keep a bounded recent log, instead of letting a noisy core fill the VPS disk.
// stdout and stderr share the same writer and therefore the same size budget.
type boundedLogWriter struct {
	mu   sync.Mutex
	file *os.File
	size int64
}

func (w *boundedLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	accepted := len(data)
	if len(data) > maxCoreLogBytes {
		data = data[len(data)-maxCoreLogBytes:]
	}
	if w.size+int64(len(data)) > maxCoreLogBytes {
		if err := w.file.Truncate(0); err != nil {
			return 0, err
		}
		if _, err := w.file.Seek(0, 0); err != nil {
			return 0, err
		}
		w.size = 0
	}
	written, err := w.file.Write(data)
	w.size += int64(written)
	if err != nil {
		return written, err
	}
	return accepted, nil
}
