//go:build !linux

package hostinventory

import (
	"context"
	"errors"
)

func readSmall(string, int) ([]byte, error) { return nil, errors.New("Linux inventory unavailable") }
func queryService(context.Context, string) ([]byte, error) {
	return nil, errors.New("Linux inventory unavailable")
}
