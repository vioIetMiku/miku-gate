package tunnel

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"time"

	"mikugate/internal/dnswire"
	"mikugate/internal/egress"
)

var ErrScope = errors.New("tunnel worker is not in the expected non-host namespace or bound interface changed")
var ErrDestination = errors.New("destination is not an allowed public IPv4 target")
var ErrDNS = errors.New("tunnel DNS failed; no host DNS fallback is permitted")
var ErrUnsupported = errors.New("tunnel runtime requires Linux")

// NamespaceID is supplied by the parent before exec into the worker namespace.
// Workers must be separate processes; this code never setns-es a Go thread.
type NamespaceID struct {
	Device uint64
	Inode  uint64
}
type DialOptions struct {
	Expected  NamespaceID
	Host      NamespaceID
	Interface string
	Resolver  netip.AddrPort
}
type Dialer struct {
	scope   func() error
	policy  func(string) error
	lookup  func(context.Context, string) ([]netip.Addr, error)
	connect func(context.Context, string, string) (net.Conn, error)
}

// NewWorkerDialer must be called inside a separately exec'd namespace worker.
// No default namespace, interface or resolver is ever inferred.
func NewWorkerDialer(options DialOptions) (*Dialer, error) {
	return newWorkerDialer(options, egress.PublicIPv4)
}
func newWorkerDialer(options DialOptions, policy func(string) error) (*Dialer, error) {
	resolver := options.Resolver.Addr()
	if !options.Resolver.IsValid() || !resolver.Is4() || resolver.IsUnspecified() || resolver.IsLoopback() || resolver.IsLinkLocalUnicast() || resolver.IsMulticast() || options.Resolver.Port() == 0 {
		return nil, ErrScope
	}
	scope, connect, err := workerNetwork(options)
	if err != nil {
		return nil, err
	}
	d := &Dialer{scope: scope, policy: policy, connect: connect}
	d.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return lookupDNS(ctx, host, options.Resolver, connect)
	}
	return d, nil
}
func validHostname(host string) bool { return dnswire.ValidHostname(host) }

// DialContext satisfies the existing proxy dialer API, but is not wired to an
// entrance listener. DNS results are checked as a set before any TCP dial, and
// only literal vetted IPs reach connect (no second resolution / rebinding gap).
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp4" {
		return nil, ErrDestination
	}
	if err := d.scope(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDestination
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 || strconv.Itoa(int(value)) != port {
		return nil, ErrDestination
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		if !validHostname(host) {
			return nil, ErrDestination
		}
		addresses, err = d.lookup(ctx, host)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(ErrDNS, ctx.Err())
			}
			return nil, ErrDNS
		}
	}
	if len(addresses) == 0 || len(addresses) > 8 {
		return nil, ErrDNS
	}
	// Fail the entire answer if any address violates policy, not only the first.
	for _, address := range addresses {
		if !address.Is4() || d.policy(address.String()) != nil {
			return nil, ErrDestination
		}
	}
	if err = d.scope(); err != nil {
		return nil, err
	}
	var failure error
	for _, address := range addresses {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err := d.connect(ctx, "tcp4", net.JoinHostPort(address.String(), port))
		if err == nil {
			return conn, nil
		}
		failure = err
	}
	return nil, failure
}
