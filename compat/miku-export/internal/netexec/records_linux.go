//go:build linux

package netexec

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

var recordName = regexp.MustCompile(`^mg-[a-f0-9]{20}$`)

type diskRecords struct{ directory string }

func (d *diskRecords) filename(name string) (string, error) {
	if !recordName.MatchString(name) {
		return "", ErrOwnership
	}
	return filepath.Join(d.directory, name+".json"), nil
}
func (d *diskRecords) Load(name string) (record, bool, error) {
	path, err := d.filename(name)
	if err != nil {
		return record{}, false, err
	}
	// A corrupt FIFO must not block before secureFile can reject its type.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if err = secureFile(file); err != nil {
		return record{}, true, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return record{}, true, err
	}
	if len(raw) > 8192 {
		return record{}, true, errors.New("ownership record exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var r record
	if err = dec.Decode(&r); err != nil {
		return r, true, fmt.Errorf("invalid ownership record: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return r, true, errors.New("trailing data in ownership record")
	}
	return r, true, nil
}
func (d *diskRecords) Save(name string, r record) error {
	path, err := d.filename(name)
	if err != nil {
		return err
	}
	p, err := BuildPlan(r.InstanceID, r.ExitID)
	if err != nil || p.Namespace != name || r.Namespace != name || r.Schema != Schema || r.CreatedAt.IsZero() {
		return ErrOwnership
	}
	if _, _, err = d.Load(name); err != nil {
		return err
	}
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(d.directory, ".tmp-"+hex.EncodeToString(nonce[:]))
	fd, err := unix.Open(tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "ownership-record")
	defer func() { file.Close(); _ = os.Remove(tmp) }()
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDirectory(d.directory)
}
func (d *diskRecords) Delete(name string) error {
	path, err := d.filename(name)
	if err != nil {
		return err
	}
	if _, found, err := d.Load(name); err != nil {
		return err
	} else if !found {
		return nil
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(d.directory)
}
func (d *diskRecords) Count() (int, error) {
	entries, err := os.ReadDir(d.directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.Name() == ".lock" || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !recordName.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
			return 0, ErrOwnership
		}
		count++
	}
	return count, nil
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
