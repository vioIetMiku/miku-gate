package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCoreLogIsBoundedAndRetainsLatestOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := &boundedLogWriter{file: file}
	if _, err = writer.Write(bytes.Repeat([]byte("old"), maxCoreLogBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("latest failure detail\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > maxCoreLogBytes || !bytes.Contains(raw, []byte("latest failure detail")) {
		t.Fatal("log limit or recent output missing", len(raw), err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				if _, err := writer.Write(bytes.Repeat([]byte("x"), 65536)); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	workers.Wait()
	info, err := file.Stat()
	if err != nil || info.Size() > maxCoreLogBytes {
		t.Fatal("concurrent core output exceeded limit", err)
	}
}
