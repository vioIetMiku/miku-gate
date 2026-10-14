//go:build !linux

package executoripc

import (
	"context"
	"net"
)

func DialVPN(context.Context, string) (net.Conn, error) { return nil, ErrUnsupported }
