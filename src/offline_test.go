package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

// Unit tests use fixture public addresses and loopback HTTP servers only.
func TestMain(m *testing.M) {
	setPublicIPOverride("192.0.2.1")
	http.DefaultTransport = &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("external HTTP disabled in offline tests")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	os.Exit(m.Run())
}
