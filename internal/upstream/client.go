// Package upstream connects business TCP only through a configured proxy. Business
// DNS uses authenticated DoH over that same proxy; there is no direct fallback.
package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/egress"
)

var ErrEndpoint = errors.New("代理上游地址无效或不可达")
var ErrAuthentication = errors.New("代理上游认证失败")
var ErrProtocol = errors.New("代理上游未返回有效的连接协议")
var ErrTLS = errors.New("代理上游或DNS服务的TLS身份无法验证")
var ErrDNS = errors.New("经上游的加密DNS失败；未使用宿主DNS后备")
var ErrDestination = errors.New("目标必须是已校验的公网IPv4服务")
var ErrClosed = errors.New("代理上游已关闭")

type Client struct {
	config      domain.UpstreamConfig
	endpoints   []string
	roots       *x509.CertPool
	life        context.Context
	cancel      context.CancelFunc
	transport   *http.Transport
	http        *http.Client
	mu          sync.Mutex
	connections map[*trackedConn]struct{}
	closed      bool
}
type options struct {
	roots   *x509.CertPool // Explicit test trust anchor; never populated by API input.
	resolve func(context.Context, string) ([]netip.Addr, error)
}

func New(ctx context.Context, config domain.UpstreamConfig) (*Client, error) {
	return newClient(ctx, config, options{})
}
func newClient(ctx context.Context, config domain.UpstreamConfig, opts options) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	resolve := opts.resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		}
	}
	ips := []netip.Addr{}
	if literal, err := netip.ParseAddr(config.Host); err == nil {
		ips = append(ips, literal)
	} else {
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		var err error
		ips, err = resolve(bounded, config.Host)
		cancel()
		if err != nil {
			return nil, ErrEndpoint
		}
	}
	if len(ips) == 0 || len(ips) > 8 {
		return nil, ErrEndpoint
	}
	endpoints := []string{}
	for _, ip := range ips {
		ip = ip.Unmap()
		// Only the exact operator-confirmed loopback literal may be private. A domain
		// resolving to loopback/private space is rejected even when the toggle is on.
		local := config.AllowLoopback && config.Host == "127.0.0.1" && ip.String() == "127.0.0.1"
		if !ip.Is4() || (!local && egress.PublicIPv4(ip.String()) != nil) {
			return nil, ErrEndpoint
		}
		endpoints = append(endpoints, net.JoinHostPort(ip.String(), strconv.Itoa(config.Port)))
	}
	life, cancel := context.WithCancel(ctx)
	c := &Client{config: config, endpoints: endpoints, roots: opts.roots, life: life, cancel: cancel, connections: map[*trackedConn]struct{}{}}
	c.setupDNS()
	context.AfterFunc(life, func() { c.Close() })
	return c, nil
}
func (c *Client) bounded(ctx context.Context) (context.Context, func()) {
	bound, cancel := context.WithTimeout(ctx, 10*time.Second)
	stop := context.AfterFunc(c.life, cancel)
	return bound, func() { stop(); cancel() }
}
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	all := make([]*trackedConn, 0, len(c.connections))
	for conn := range c.connections {
		all = append(all, conn)
	}
	c.mu.Unlock()
	c.cancel()
	for _, conn := range all {
		conn.Close()
	}
	c.transport.CloseIdleConnections()
	return nil
}

type trackedConn struct {
	net.Conn
	owner *Client
	once  sync.Once
}

func (c *trackedConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.owner.mu.Lock(); delete(c.owner.connections, c); c.owner.mu.Unlock() })
	return err
}
func (c *trackedConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
func (c *Client) track(conn net.Conn) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.life.Err() != nil {
		conn.Close()
		return nil, ErrClosed
	}
	wrapped := &trackedConn{Conn: conn, owner: c}
	c.connections[wrapped] = struct{}{}
	return wrapped, nil
}
func (c *Client) endpoint(ctx context.Context) (net.Conn, error) {
	if c.life.Err() != nil {
		return nil, ErrClosed
	}
	for _, address := range c.endpoints {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dialer := net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
		raw, err := dialer.DialContext(ctx, "tcp4", address)
		if err != nil {
			continue
		}
		conn, err := c.track(raw)
		if err != nil {
			return nil, err
		}
		if c.config.Kind == "https" {
			name := c.config.TLSName
			if name == "" {
				name = c.config.Host
			}
			secured := tls.Client(conn, &tls.Config{ServerName: name, RootCAs: c.roots, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
			stop := context.AfterFunc(ctx, func() { secured.Close() })
			err = secured.HandshakeContext(ctx)
			stop()
			if err != nil {
				secured.Close()
				return nil, ErrTLS
			}
			return secured, nil
		}
		return conn, nil
	}
	return nil, ErrEndpoint
}
func (c *Client) connectNumeric(ctx context.Context, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || egress.PublicIPv4(host) != nil {
		return nil, ErrDestination
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 || strconv.Itoa(int(number)) != port {
		return nil, ErrDestination
	}
	conn, err := c.endpoint(ctx)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	deadline, has := ctx.Deadline()
	if !has {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err = conn.SetDeadline(deadline); err != nil {
		stop()
		conn.Close()
		return nil, ErrEndpoint
	}
	var ready net.Conn
	if c.config.Kind == "socks5" {
		ready, err = socksConnect(conn, c.config, host, uint16(number))
	} else {
		ready, err = httpConnect(conn, c.config, address)
	}
	stop()
	if err != nil || ctx.Err() != nil {
		conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, ErrEndpoint
	}
	return ready, nil
}
func (c *Client) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp4" {
		return nil, ErrDestination
	}
	if c.life.Err() != nil {
		return nil, ErrClosed
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDestination
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 || strconv.Itoa(int(number)) != port {
		return nil, ErrDestination
	}
	bounded, done := c.bounded(ctx)
	defer done()
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if !domain.PublicHostname(host) {
			return nil, ErrDestination
		}
		ips, err = c.lookup(bounded, host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 || len(ips) > 8 {
		return nil, ErrDNS
	}
	for _, ip := range ips {
		if !ip.Is4() || egress.PublicIPv4(ip.String()) != nil {
			return nil, ErrDestination
		}
	}
	var last error
	for _, ip := range ips {
		conn, err := c.connectNumeric(bounded, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// Probe verifies the upstream protocol and authenticated DNS path, not residential
// identity or a public exit IP. More detailed egress observations have separate fields.
func (c *Client) Probe(ctx context.Context) error {
	bounded, done := c.bounded(ctx)
	defer done()
	ips, err := c.lookup(bounded, "example.com")
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return ErrDNS
	}
	for _, ip := range ips {
		if !ip.Is4() || egress.PublicIPv4(ip.String()) != nil {
			return ErrDestination
		}
	}
	return nil
}
