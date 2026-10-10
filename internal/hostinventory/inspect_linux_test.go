//go:build linux

package hostinventory

import (
	"context"
	"strings"
	"testing"
)

func TestNativeQueryRejectsArbitraryUnitAndFile(t *testing.T) {
	if _, err := queryService(context.Background(), "xray.service; touch /tmp/bad"); err == nil {
		t.Fatal("non-allowlisted unit")
	}
	if _, err := readSmall("/etc/shadow", 16384); err == nil {
		t.Fatal("arbitrary read")
	}
	b := new(limitedOutput)
	if n, err := b.Write([]byte(strings.Repeat("x", 16384))); err != nil || n != 16384 {
		t.Fatal("valid bound")
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("overflow")
	}
}
