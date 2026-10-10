package vpnipc

import (
	"context"
	"crypto/tls"
	"io"
	"mikugate/internal/egress"
	"mikugate/internal/executoripc"
	"mikugate/internal/tunnel"
	"mikugate/internal/wirejson"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct{ instance string }

func New(instance string) (*Client, error) {
	if _, err := executoripc.VPNSocketPath(instance); err != nil {
		return nil, err
	}
	return &Client{instance}, nil
}
func (c *Client) exchange(ctx context.Context, request request, timeout time.Duration) (net.Conn, response, error) {
	if !request.valid() {
		return nil, response{}, ErrProtocol
	}
	bound, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := executoripc.DialVPN(bound, c.instance)
	if err != nil {
		return nil, response{}, ErrUnavailable
	}
	fail := func(err error) (net.Conn, response, error) { conn.Close(); return nil, response{}, err }
	stop := context.AfterFunc(bound, func() { conn.Close() })
	defer stop()
	deadline, _ := bound.Deadline()
	conn.SetDeadline(deadline)
	if wirejson.Write(conn, request, MaxRequest) != nil {
		return fail(ErrUnavailable)
	}
	var value response
	if err = wirejson.Read(conn, &value, MaxResponse); err != nil {
		if bound.Err() != nil {
			return fail(bound.Err())
		}
		return fail(ErrUnavailable)
	}
	if value.Version != Version {
		return fail(ErrProtocol)
	}
	if value.Error != "" {
		if value.Info != nil || value.Capabilities != nil || value.Token != "" {
			return fail(ErrProtocol)
		}
		return fail(resultError(value.Error))
	}
	if !stop() || bound.Err() != nil {
		return fail(ErrUnavailable)
	}
	conn.SetDeadline(time.Time{})
	return conn, value, nil
}
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	conn, reply, err := c.exchange(ctx, request{Version: Version, Operation: "capabilities"}, 3*time.Second)
	if err != nil {
		return Capabilities{}, err
	}
	conn.Close()
	if reply.Info != nil || reply.Token != "" || reply.Capabilities == nil {
		return Capabilities{}, ErrProtocol
	}
	caps := *reply.Capabilities
	if caps.Version != Version || caps.Instance != c.instance || caps.Mode != "isolated-openvpn" || caps.Options.Validate() != nil || caps.MaxSessions != 3 {
		return Capabilities{}, ErrProtocol
	}
	return caps, nil
}

type Session struct {
	client  *Client
	token   string
	control net.Conn
	info    tunnel.SessionInfo
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	streams map[*businessConn]struct{}
}
type businessConn struct {
	net.Conn
	session *Session
	once    sync.Once
}

func (c *businessConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.session.mu.Lock(); delete(c.session.streams, c); c.session.mu.Unlock() })
	return err
}
func (c *businessConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
func (c *Client) Start(ctx context.Context, exit string, config Config) (*Session, error) {
	life, cancel := context.WithCancel(ctx)
	conn, reply, err := c.exchange(life, request{Version: Version, Operation: "start", ExitID: exit, Profile: config.Profile, IP: config.IP, PeerName: config.PeerName, Legacy: config.AllowLegacy}, 28*time.Second)
	if err != nil {
		cancel()
		return nil, err
	}
	if reply.Info == nil || reply.Capabilities != nil || !tokenPattern.MatchString(reply.Token) || reply.Info.Namespace.Inode == 0 || reply.Info.InterfaceIndex == 0 || reply.Info.TunnelUID == 0 || reply.Info.WorkerUID == 0 {
		conn.Close()
		cancel()
		return nil, ErrProtocol
	}
	session := &Session{client: c, token: reply.Token, control: conn, info: *reply.Info, ctx: life, cancel: cancel, done: make(chan struct{}), streams: map[*businessConn]struct{}{}}
	go func() { var b [1]byte; conn.Read(b[:]); cancel(); session.closeLocal(); close(session.done) }()
	go func() { <-life.Done(); session.closeLocal() }()
	return session, nil
}
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) closeLocal() {
	s.mu.Lock()
	all := make([]*businessConn, 0, len(s.streams))
	for conn := range s.streams {
		all = append(all, conn)
	}
	s.mu.Unlock()
	s.control.Close()
	for _, conn := range all {
		conn.Close()
	}
}
func (s *Session) Close() error {
	var result error
	s.once.Do(func() {
		s.cancel()
		s.closeLocal()
		ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		conn, reply, err := s.client.exchange(ctx, request{Version: Version, Operation: "stop", Token: s.token}, 5*time.Second)
		if err != nil {
			result = err
			return
		}
		conn.Close()
		if reply.Info != nil || reply.Capabilities != nil || reply.Token != "" {
			result = ErrProtocol
		}
	})
	return result
}
func (s *Session) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp4" || s.ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	conn, reply, err := s.client.exchange(ctx, request{Version: Version, Operation: "dial", Token: s.token, Address: address}, 7*time.Second)
	if err != nil {
		return nil, err
	}
	if reply.Token != "" || reply.Info != nil || reply.Capabilities != nil {
		conn.Close()
		return nil, ErrProtocol
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		conn.Close()
		return nil, ErrUnavailable
	}
	wrapped := &businessConn{Conn: conn, session: s}
	s.streams[wrapped] = struct{}{}
	return wrapped, nil
}
func (s *Session) Verify(ctx context.Context) error {
	if s.ctx.Err() != nil {
		return ErrUnavailable
	}
	conn, reply, err := s.client.exchange(ctx, request{Version: Version, Operation: "verify", Token: s.token}, 6*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	if reply.Info == nil || *reply.Info != s.info || reply.Capabilities != nil || reply.Token != "" {
		return ErrProtocol
	}
	return nil
}

// Probe tests DNS, connected TCP and verified HTTPS through the namespace worker,
// then compares the observed guard identity again. Never uses a system dialer.
func (s *Session) Probe(ctx context.Context) (string, error) {
	if err := s.Verify(ctx); err != nil {
		return "", err
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 8192, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return s.DialContext(ctx, "tcp4", address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProtocol }}
	request, err := http.NewRequestWithContext(ctx, "GET", "https://one.one.one.one/cdn-cgi/trace", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(data) > 8192 {
		return "", ErrProtocol
	}
	ip := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ip=") {
			if ip != "" {
				return "", ErrProtocol
			}
			ip = strings.TrimPrefix(line, "ip=")
		}
	}
	if egress.PublicIPv4(ip) != nil {
		return "", ErrProtocol
	}
	if err = s.Verify(ctx); err != nil {
		return "", err
	}
	return ip, nil
}
