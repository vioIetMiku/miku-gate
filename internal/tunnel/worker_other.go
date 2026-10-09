//go:build !linux

package tunnel

import (
	"context"
	"net"
)

func CurrentNamespace() (NamespaceID, error) { return NamespaceID{}, ErrUnsupported }
func workerNetwork(DialOptions) (func() error, func(context.Context, string, string) (net.Conn, error), error) {
	return nil, nil, ErrUnsupported
}
