package tunnel

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"mikugate/internal/egress"
)

func TestDialerValidatesAllResolvedIPsBeforeTCP(t *testing.T) {
	for _, name := range []string{"valid", "private", "mixed", "ipv6", "empty", "too-many", "dns-error", "scope-lost"} {
		t.Run(name, func(t *testing.T) {
			addresses := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
			var resolveErr error
			switch name {
			case "private":
				addresses = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
			case "mixed":
				addresses = append(addresses, netip.MustParseAddr("169.254.169.254"))
			case "ipv6":
				addresses = []netip.Addr{netip.MustParseAddr("::ffff:8.8.8.8")}
			case "empty":
				addresses = nil
			case "too-many":
				for len(addresses) < 9 {
					addresses = append(addresses, addresses[0])
				}
			case "dns-error":
				resolveErr = errors.New("resolver not reachable")
			}
			queries, calls, scopeCalls := 0, 0, 0
			dialer := &Dialer{scope: func() error {
				scopeCalls++
				if name == "scope-lost" && scopeCalls > 1 {
					return ErrScope
				}
				return nil
			}, policy: egress.PublicIPv4, lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
				queries++
				if host != "example.test" {
					t.Fatal(host)
				}
				return addresses, resolveErr
			}, connect: func(_ context.Context, network, address string) (net.Conn, error) {
				calls++
				if network != "tcp4" || address != "8.8.8.8:443" {
					t.Fatal("second hostname resolution would be possible", network, address)
				}
				a, b := net.Pipe()
				b.Close()
				return a, nil
			}}
			conn, err := dialer.DialContext(t.Context(), "tcp4", "example.test:443")
			if name == "valid" {
				if err != nil || calls != 1 {
					t.Fatal(err, calls)
				}
				conn.Close()
			} else if err == nil || calls != 0 {
				t.Fatal("invalid answers reached the TCP network", err, calls)
			}
			if queries != 1 {
				t.Fatal("extra DNS query or fallback", queries)
			}
		})
	}
}
func TestDialerLiteralAndInputPolicies(t *testing.T) {
	calls := 0
	dialer := &Dialer{scope: func() error { return nil }, policy: egress.PublicIPv4, lookup: func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("literal should not resolve")
		return nil, nil
	}, connect: func(_ context.Context, network, address string) (net.Conn, error) {
		calls++
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}}
	for _, address := range []string{"127.0.0.1:80", "169.254.169.254:80", "8.8.8.8:0", "8.8.8.8:65536", "8.8.8.8:00443", "[::ffff:8.8.8.8]:443", "bad host:443", "bad/path:443", strings.Repeat("a", 64) + ".test:80"} {
		if _, err := dialer.DialContext(t.Context(), "tcp4", address); err == nil {
			t.Fatal(address)
		}
	}
	if calls != 0 {
		t.Fatal("invalid destination dialed")
	}
	if _, err := dialer.DialContext(t.Context(), "udp4", "8.8.8.8:53"); err == nil {
		t.Fatal("business UDP unexpectedly enabled")
	}
	conn, err := dialer.DialContext(t.Context(), "tcp4", "8.8.8.8:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestNoImplicitScopeOrResolver(t *testing.T) {
	for _, options := range []DialOptions{{}, {Resolver: netip.MustParseAddrPort("127.0.0.1:53")}, {Resolver: netip.MustParseAddrPort("169.254.169.254:53")}, {Expected: NamespaceID{Inode: 1}, Host: NamespaceID{Inode: 1}, Interface: Device, Resolver: netip.MustParseAddrPort("1.1.1.1:53")}} {
		if _, err := NewWorkerDialer(options); err == nil {
			t.Fatal("unsafe implicit options accepted")
		}
	}
}
