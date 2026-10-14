//go:build !linux

package executoripc

import (
	"context"
	"net"
)

func dialTrusted(context.Context, string) (net.Conn, error)     { return nil, ErrUnsupported }
func Serve(context.Context, string, uint32, func(string)) error { return ErrUnsupported }
