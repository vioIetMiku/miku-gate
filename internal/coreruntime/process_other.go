//go:build !linux

package coreruntime

import (
	"context"
	"mikugate/internal/ingress"
	"net"
	"strconv"
)

func joinListen(r ingress.ManagedRequest) string {
	return net.JoinHostPort(r.Listen, strconv.Itoa(r.Port))
}
func nativeRunner(Options) Runner {
	return func(context.Context, string, []byte, string) (Process, error) { return nil, ErrUnavailable }
}
