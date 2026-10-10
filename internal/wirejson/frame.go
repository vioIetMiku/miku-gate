// Package wirejson provides bounded, unambiguous JSON messages for local IPC.
package wirejson

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

var ErrFrame = errors.New("invalid bounded IPC message")

func Decode(raw []byte, value any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return ErrFrame
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	count := 0
	var visit func(int) bool
	visit = func(depth int) bool {
		count++
		if depth > 8 || count > 2048 {
			return false
		}
		token, err := scan.Token()
		if err != nil {
			return false
		}
		delim, container := token.(json.Delim)
		if !container {
			return depth > 0
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for scan.More() {
				key, err := scan.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || keys[name] || name == "" {
					return false
				}
				for _, c := range name {
					if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
						return false
					}
				}
				keys[name] = true
				if !visit(depth + 1) {
					return false
				}
			}
			end, err := scan.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			if depth == 0 {
				return false
			}
			for scan.More() {
				if !visit(depth + 1) {
					return false
				}
			}
			end, err := scan.Token()
			return err == nil && end == json.Delim(']')
		}
		return false
	}
	if !visit(0) {
		return ErrFrame
	}
	if _, err := scan.Token(); err != io.EOF {
		return ErrFrame
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return ErrFrame
	}
	return nil
}
func Read(reader io.Reader, value any, limit int) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || uint64(size) > uint64(limit) {
		return ErrFrame
	}
	raw := make([]byte, int(size))
	if _, err := io.ReadFull(reader, raw); err != nil {
		return err
	}
	return Decode(raw, value, limit)
}
func Write(writer io.Writer, value any, limit int) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > limit {
		return ErrFrame
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	_, err = io.Copy(writer, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(raw)))
	return err
}
