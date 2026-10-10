//go:build !linux

package vpnipc

import (
	"context"
	"mikugate/internal/tunnel"
)

func Serve(context.Context, string, uint32, tunnel.SessionOptions, func(string)) error {
	return tunnel.ErrUnsupported
}
