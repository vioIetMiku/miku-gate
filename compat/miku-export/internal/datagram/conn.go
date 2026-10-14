// Package datagram preserves packet boundaries across private stream IPC.
package datagram

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
)

const Maximum = 8192

type Conn struct {
	net.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
}

func Wrap(c net.Conn) *Conn { return &Conn{Conn: c} }
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
func (c *Conn) Write(p []byte) (int, error) {
	if len(p) > Maximum {
		return 0, errors.New("datagram exceeds budget")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(p)))
	if err := writeAll(c.Conn, header[:]); err != nil {
		return 0, err
	}
	if err := writeAll(c.Conn, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
func (c *Conn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var header [2]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	size := int(binary.BigEndian.Uint16(header[:]))
	if size > Maximum {
		return 0, errors.New("invalid datagram frame")
	}
	if size > len(p) {
		if _, err := io.CopyN(io.Discard, c.Conn, int64(size)); err != nil {
			return 0, err
		}
		return 0, io.ErrShortBuffer
	}
	return io.ReadFull(c.Conn, p[:size])
}
func Bridge(ctx context.Context, stream, packet net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	framed := Wrap(stream)
	stop := context.AfterFunc(ctx, func() { stream.Close(); packet.Close() })
	defer stop()
	var wg sync.WaitGroup
	copyPackets := func(dst, src net.Conn) {
		defer wg.Done()
		defer cancel()
		buffer := make([]byte, Maximum+1)
		for {
			n, err := src.Read(buffer)
			if err != nil || n > Maximum {
				return
			}
			if _, err = dst.Write(buffer[:n]); err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go copyPackets(framed, packet)
	go copyPackets(packet, framed)
	wg.Wait()
}
