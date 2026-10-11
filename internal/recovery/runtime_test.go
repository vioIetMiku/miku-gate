package recovery_test

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/recovery"
	"mikugate/internal/store"
	"net"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type session struct{ bad *atomic.Bool }

func (s *session) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("controlled fixture never forwards business")
}
func (s *session) Probe(context.Context) (gateway.Proof, error) {
	if s.bad.Load() {
		return gateway.Proof{}, errors.New("fixture lost exit")
	}
	return gateway.Proof{Kind: "https", ProtectionMode: "proxy_only", DNSVerified: true}, nil
}
func (s *session) Close() error { return nil }
func TestActualGatewayReconnectKeepsPortCredentialsAndPause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	temp, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	_, rawPort, _ := net.SplitHostPort(temp.Addr().String())
	port, _ := strconv.Atoi(rawPort)
	temp.Close()
	exit, password, e := s.CreateExit(domain.ExitInput{Name: "stable", Country: "JP", Port: port})
	if e != nil {
		t.Fatal(e)
	}
	_, hashBefore, _ := s.ExitForRuntime(exit.ID)
	if e = s.SaveUpstream(exit.ID, domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443}); e != nil {
		t.Fatal(e)
	}
	var bad atomic.Bool
	bad.Store(true)
	var starts atomic.Int32
	runtime, e := gateway.New(ctx, s, gateway.Options{Factory: func(context.Context, domain.Exit, *domain.UpstreamConfig) (gateway.Session, error) {
		starts.Add(1)
		return &session{bad: &bad}, nil
	}, HealthInterval: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close(context.Background())
	if e = s.SaveRecovery(exit.ID, domain.RecoveryPolicy{Enabled: true, Confirm: true}); e != nil {
		t.Fatal(e)
	}
	automatic := recovery.New(ctx, s, runtime, recovery.Options{Interval: 5 * time.Millisecond, RetryMin: 50 * time.Millisecond, RetryMax: 100 * time.Millisecond})
	defer automatic.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && starts.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, ok := runtime.Snapshot(exit.ID); ok {
		t.Fatal("failed proof opened listener")
	}
	bad.Store(false)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, ok := runtime.Snapshot(exit.ID)
		if ok && state.Healthy && state.ProxyListening {
			break
		}
		time.Sleep(time.Millisecond)
	}
	state, ok := runtime.Snapshot(exit.ID)
	if !ok || !state.Healthy || state.Listen != "127.0.0.1:"+rawPort {
		t.Fatal("actual listener wasn't restored", state)
	}
	after, hashAfter, _ := s.ExitForRuntime(exit.ID)
	if hashAfter != hashBefore || after.Port != exit.Port || after.Username != exit.Username {
		t.Fatal("reconnect rotated logical entry")
	}
	// Existing credentials authenticate on the actual listener after recovery.
	connection, e := net.DialTimeout("tcp4", state.Listen, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(time.Second))
	connection.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	if _, e = connection.Read(reply); e != nil || reply[0] != 5 || reply[1] != 2 {
		t.Fatal("recovered SOCKS auth")
	}
	request := append([]byte{1, byte(len(after.Username))}, []byte(after.Username)...)
	request = append(request, byte(len(password)))
	request = append(request, []byte(password)...)
	connection.Write(request)
	if _, e = connection.Read(reply); e != nil || reply[0] != 1 || reply[1] != 0 {
		t.Fatal("old credential rejected after recovery")
	}
	if e = automatic.Pause(exit.ID); e != nil {
		t.Fatal(e)
	}
	if e = automatic.Control(t.Context(), exit.ID, func() error { return runtime.Stop(t.Context(), exit.ID) }); e != nil {
		t.Fatal(e)
	}
	before := starts.Load()
	time.Sleep(70 * time.Millisecond)
	if starts.Load() != before {
		t.Fatal("explicit paused exit automatically reopened")
	}
}
