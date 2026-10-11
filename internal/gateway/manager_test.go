package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/security"
	"mikugate/internal/upstream"
)

type memorySource struct {
	mu     sync.Mutex
	exits  map[string]domain.Exit
	hash   string
	status map[string]string
}

func newSource(t *testing.T) *memorySource {
	t.Helper()
	hash, err := security.HashPassword("client-secret")
	if err != nil {
		t.Fatal(err)
	}
	s := &memorySource{exits: map[string]domain.Exit{}, hash: hash, status: map[string]string{}}
	for _, id := range []string{"one", "two", "three"} {
		s.exits[id] = domain.Exit{ID: id, Username: "client", Listen: "127.0.0.1", Port: 20001, Policy: "any"}
	}
	return s
}
func (s *memorySource) ExitForRuntime(id string) (domain.Exit, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.exits[id]
	if !ok {
		return e, "", domain.ErrNotFound
	}
	return e, s.hash, nil
}
func (s *memorySource) Upstream(id string) (domain.UpstreamConfig, string, bool, error) {
	_, _, err := s.ExitForRuntime(id)
	return domain.UpstreamConfig{Kind: "https", Host: "proxy.example", Port: 443}, "", true, err
}
func (s *memorySource) SetStatus(id, status, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.exits[id]; !ok {
		return domain.ErrNotFound
	}
	s.status[id] = status
	return nil
}
func (s *memorySource) AddEvent(string, string, string, string) error { return nil }

type fakeSession struct {
	probe  func(context.Context) (Proof, error)
	dial   proxyDial
	closed atomic.Int32
}
type proxyDial func(context.Context, string, string) (net.Conn, error)

func (s *fakeSession) Probe(ctx context.Context) (Proof, error) {
	if s.probe != nil {
		return s.probe(ctx)
	}
	return Proof{Kind: "http", ProtectionMode: "proxy_only", DNSVerified: true}, nil
}
func (s *fakeSession) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if s.dial == nil {
		return nil, upstream.ErrEndpoint
	}
	return s.dial(ctx, network, address)
}
func (s *fakeSession) Close() error { s.closed.Add(1); return nil }
func managerFor(t *testing.T, s Source, options Options) *Manager {
	t.Helper()
	if options.Listen == nil {
		options.Listen = func(ctx context.Context, _, _ string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
		}
	}
	if options.HealthInterval == 0 {
		options.HealthInterval = time.Hour
	}
	m, err := New(t.Context(), s, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func TestActualListenerAuthenticationAndSynchronousStop(t *testing.T) {
	source := newSource(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "actual gateway traffic") }))
	defer target.Close()
	session := &fakeSession{dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Listener.Addr().String())
	}}
	var factories atomic.Int32
	m := managerFor(t, source, Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) {
		factories.Add(1)
		return session, nil
	}})
	state, err := m.Start(t.Context(), "one")
	if err != nil || !state.ProxyListening || !state.Healthy || state.Proof.KernelVerified {
		t.Fatal("not a real truthful runtime", state, err)
	}
	again, err := m.Start(t.Context(), "one")
	if err != nil || again.Listen != state.Listen || factories.Load() != 1 {
		t.Fatal("start not idempotent")
	}
	client, err := net.Dial("tcp4", state.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(client, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nProxy-Authorization: Basic Y2xpZW50OmNsaWVudC1zZWNyZXQ=\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "actual gateway traffic" {
		t.Fatal("data path failed", err)
	}
	if err = m.WithStopped("one", func() error { t.Fatal("running configuration mutated"); return nil }); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	idle, err := net.Dial("tcp4", state.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	if err = m.Stop(t.Context(), "one"); err != nil {
		t.Fatal(err)
	}
	if session.closed.Load() != 1 {
		t.Fatal("session not closed exactly once", session.closed.Load())
	}
	if _, ok := m.Snapshot("one"); ok {
		t.Fatal("runtime survived stop")
	}
	if next, err := net.DialTimeout("tcp4", state.Listen, 100*time.Millisecond); err == nil {
		next.Close()
		t.Fatal("listener survived stop")
	}
	idle.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err = idle.Read(b[:]); err == nil {
		t.Fatal("accepted connection survived stop")
	}
	source.mu.Lock()
	persisted := source.status["one"]
	source.mu.Unlock()
	if persisted != "stopped" {
		t.Fatal("stopped state not persisted")
	}
}
func TestFailedProbeAndInvalidProofDoNotOpenListener(t *testing.T) {
	for _, kind := range []string{"failure", "false-proof", "nil-session"} {
		t.Run(kind, func(t *testing.T) {
			source := newSource(t)
			var listened atomic.Int32
			session := &fakeSession{probe: func(context.Context) (Proof, error) {
				if kind == "failure" {
					return Proof{}, upstream.ErrDNS
				}
				return Proof{Kind: "http", ProtectionMode: "proxy_only", DNSVerified: true, KernelVerified: true}, nil
			}}
			m := managerFor(t, source, Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) {
				if kind == "nil-session" {
					return nil, nil
				}
				return session, nil
			}, Listen: func(context.Context, string, string) (net.Listener, error) {
				listened.Add(1)
				return nil, errors.New("must not listen")
			}})
			if _, err := m.Start(t.Context(), "one"); err == nil {
				t.Fatal("unverified start succeeded")
			}
			if listened.Load() != 0 {
				t.Fatal("listener opened before proof")
			}
			if _, ok := m.Snapshot("one"); ok {
				t.Fatal("failed entry retained")
			}
			if kind != "nil-session" && session.closed.Load() != 1 {
				t.Fatal("failed session leaked or double-closed", session.closed.Load())
			}
		})
	}
}
func TestCancellationAndConcurrentStart(t *testing.T) {
	source := newSource(t)
	entered := make(chan struct{})
	session := &fakeSession{probe: func(ctx context.Context) (Proof, error) { close(entered); <-ctx.Done(); return Proof{}, ctx.Err() }}
	m := managerFor(t, source, Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) { return session, nil }})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := m.Start(ctx, "one"); done <- err }()
	<-entered
	if _, err := m.Start(t.Context(), "one"); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel startup accepted", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not finish startup")
	}
	if session.closed.Load() != 1 {
		t.Fatal("canceled startup did not close session")
	}
	if _, ok := m.Snapshot("one"); ok {
		t.Fatal("canceled startup survived")
	}
}
func TestReservationDoesNotBlockOtherExitsAndCapacity(t *testing.T) {
	source := newSource(t)
	m := managerFor(t, source, Options{MaxExits: 1, Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) {
		return &fakeSession{}, nil
	}})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- m.WithStopped("one", func() error { close(entered); <-release; return nil }) }()
	<-entered
	if _, err := m.Start(t.Context(), "one"); !errors.Is(err, ErrBusy) {
		t.Fatal("reserved exit started", err)
	}
	if _, err := m.Start(t.Context(), "two"); err != nil {
		t.Fatal("global lock blocked another exit", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(t.Context(), "one"); !errors.Is(err, ErrCapacity) {
		t.Fatal("capacity ignored", err)
	}
	if err := m.Stop(t.Context(), "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(t.Context(), "one"); err != nil {
		t.Fatal("capacity not released", err)
	}
}
func TestPortConflictKeepsExistingListener(t *testing.T) {
	source := newSource(t)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, port, _ := net.SplitHostPort(occupied.Addr().String())
	value, _ := strconv.Atoi(port)
	e := source.exits["one"]
	e.Port = value
	source.exits["one"] = e
	session := &fakeSession{}
	m := managerFor(t, source, Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) { return session, nil }, Listen: (&net.ListenConfig{}).Listen})
	if _, err = m.Start(t.Context(), "one"); !errors.Is(err, ErrListen) {
		t.Fatal("port collision not reported", err)
	}
	check, err := net.Dial("tcp4", occupied.Addr().String())
	if err != nil {
		t.Fatal("existing listener damaged", err)
	}
	check.Close()
	if session.closed.Load() != 1 {
		t.Fatal("session leaked on collision")
	}
}
func TestHealthFailureWithdrawsListenerBeforeExplicitReconnection(t *testing.T) {
	source := newSource(t)
	var bad atomic.Bool
	factory := func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) {
		return &fakeSession{probe: func(context.Context) (Proof, error) {
			if bad.Load() {
				return Proof{}, upstream.ErrDNS
			}
			return Proof{Kind: "socks5", ProtectionMode: "proxy_only", DNSVerified: true}, nil
		}}, nil
	}
	m := managerFor(t, source, Options{Factory: factory})
	start, e := m.Start(t.Context(), "one")
	if e != nil {
		t.Fatal(e)
	}
	bad.Store(true)
	state, e := m.Check(t.Context(), "one")
	if !errors.Is(e, upstream.ErrDNS) || state.Healthy || state.ProxyListening || state.Proof.DNSVerified {
		t.Fatal("failed health still forwarded", state, e)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.Snapshot("one"); !ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := m.Snapshot("one"); ok {
		t.Fatal("failed health still owns old session")
	}
	if connection, e := net.DialTimeout("tcp4", start.Listen, 100*time.Millisecond); e == nil {
		connection.Close()
		t.Fatal("failed listener still accepts")
	}
	bad.Store(false)
	state, e = m.Start(t.Context(), "one")
	if e != nil || !state.Healthy || !state.ProxyListening {
		t.Fatal("explicit reconnect not verified", state, e)
	}
}
func TestStrictResidentialDoesNotInventEvidence(t *testing.T) {
	source := newSource(t)
	e := source.exits["one"]
	e.Policy = "residential_strict"
	source.exits["one"] = e
	m := managerFor(t, source, Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error) {
		t.Fatal("unverified strict policy reached factory")
		return nil, nil
	}})
	if _, err := m.Start(t.Context(), "one"); !errors.Is(err, ErrPolicy) {
		t.Fatal(err)
	}
}
func TestAuthenticatorCachesOnlyVerifiedCredential(t *testing.T) {
	hash, err := security.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	slots := make(chan struct{}, 1)
	auth, err := newAuthenticator("u", hash, slots)
	if err != nil {
		t.Fatal(err)
	}
	if auth("u", "wrong") || auth("other", "secret") {
		t.Fatal("bad credentials accepted")
	}
	if !auth("u", "secret") {
		t.Fatal("good credentials denied")
	}
	slots <- struct{}{}
	if !auth("u", "secret") || auth("u", "wrong") {
		t.Fatal("verified cache invalid")
	}
	<-slots
	if _, err = newAuthenticator("u", "argon2id$v19$m65536t2p2$bad$bad", slots); err == nil {
		t.Fatal("invalid credential material opened runtime")
	}
}
