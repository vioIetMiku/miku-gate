// Package streamio joins owned stream connections with cancellation and half-close.
package streamio

import (
	"context"
	"io"
	"net"
	"time"
)

type reader struct {
	io.Reader
	touch func()
}

func (r reader) Read(p []byte) (int, error) { r.touch(); return r.Reader.Read(p) }

type writer struct {
	io.Writer
	touch func()
}

func (w writer) Write(p []byte) (int, error) { w.touch(); return w.Writer.Write(p) }
func Bridge(ctx context.Context, left, right net.Conn) {
	defer left.Close()
	defer right.Close()
	stop := context.AfterFunc(ctx, func() { left.Close(); right.Close() })
	defer stop()
	touch := func() {
		deadline := time.Now().Add(5 * time.Minute)
		left.SetDeadline(deadline)
		right.SetDeadline(deadline)
	}
	touch()
	done := make(chan struct{}, 2)
	copyOne := func(destination, source net.Conn) {
		_, err := io.CopyBuffer(writer{destination, touch}, reader{source, touch}, make([]byte, 16*1024))
		if err == nil {
			if half, ok := destination.(interface{ CloseWrite() error }); ok {
				err = half.CloseWrite()
			} else {
				err = io.ErrClosedPipe
			}
		}
		if err != nil {
			left.Close()
			right.Close()
		}
		done <- struct{}{}
	}
	go copyOne(left, right)
	go copyOne(right, left)
	<-done
	<-done
}
