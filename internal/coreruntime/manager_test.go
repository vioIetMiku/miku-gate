package coreruntime

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
	"strings"
	"sync"
	"testing"
	"time"
)

type sourceFixture struct {
	material ingress.ManagedMaterial
	exit     domain.Exit
	hash     string
}

func (s sourceFixture) Managed(string) (ingress.ManagedMaterial, string, bool, error) {
	return s.material, "", true, nil
}
func (s sourceFixture) ExitForRuntime(string) (domain.Exit, string, error) {
	return s.exit, s.hash, nil
}
func (sourceFixture) AddEvent(string, string, string, string) error { return nil }

type exitFixture struct {
	life   context.Context
	active bool
}

func (s exitFixture) WithActive(_ string, fn func(context.Context, gateway.State) error) error {
	if !s.active {
		return gateway.ErrBusy
	}
	return fn(s.life, gateway.State{Phase: "running", Healthy: true, ProxyListening: true})
}

type fakeProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *fakeProcess) Done() <-chan struct{}      { return p.done }
func (p *fakeProcess) PID() int                   { return 123 }
func (p *fakeProcess) Stop(context.Context) error { p.once.Do(func() { close(p.done) }); return nil }
func materialFixture(t *testing.T) sourceFixture {
	t.Helper()
	hash, _ := security.HashPassword("exit-password")
	e := domain.Exit{ID: "exit", Port: 22000, Username: "mg_test"}
	m := ingress.ManagedMaterial{Request: ingress.ManagedRequest{Core: "sing-box", Template: "vless-reality-vision", Address: "entry.example", Listen: "127.0.0.1", Port: 25003, ServerName: "target.example", RealityHostname: "target.example"}, Proxy: ingress.Proxy{Port: 22000, Username: "mg_test", Password: "exit-password"}, Identity: ingress.ManagedIdentity{UUID: "7c664910-2503-4f16-853f-636c5c68ff90", Password: strings.Repeat("a", 32), RealityPrivateKey: "aRlwIfRYFL8CDWSaxjwjorGXIZAzTy9hUXGgBFfltmI", ShortID: "071c5e3d4b09ab5e"}, Target: &ingress.RealityTarget{Hostname: "target.example", Address: "8.8.8.8", Port: 443, CheckedAt: time.Now(), NotAfter: time.Now().Add(24 * time.Hour), TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true}}
	return sourceFixture{m, e, hash}
}
func TestManagerExitLeaseMutationAndProcessLoss(t *testing.T) {
	source := materialFixture(t)
	exitLife, stopExit := context.WithCancel(context.Background())
	process := &fakeProcess{done: make(chan struct{})}
	runnerCalls := 0
	m, e := New(context.Background(), source, exitFixture{exitLife, true}, Options{Runner: func(context.Context, string, []byte, string) (Process, error) { runnerCalls++; return process, nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	if _, ok := m.Snapshot("exit"); ok {
		t.Fatal("durable config assumed running")
	}
	state, e := m.Start(context.Background(), "exit")
	if e != nil || state.Phase != "running" || state.DataVerified || runnerCalls != 1 {
		t.Fatal("wrong running scope", e)
	}
	if e = m.WithStopped("exit", func() error { t.Fatal("live mutated"); return nil }); !errors.Is(e, ErrBusy) {
		t.Fatal("missing operation lock")
	}
	stopExit()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.Snapshot("exit"); !ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := m.Snapshot("exit"); ok {
		t.Fatal("exit lost but core stays active")
	}
	select {
	case <-process.done:
	default:
		t.Fatal("child not stopped")
	}
}
func TestManagerFailedOrWrongExitNeverRunsCore(t *testing.T) {
	source := materialFixture(t)
	calls := 0
	options := Options{Runner: func(context.Context, string, []byte, string) (Process, error) { calls++; return nil, ErrCore }}
	m, _ := New(context.Background(), source, exitFixture{context.Background(), false}, options)
	if _, e := m.Start(context.Background(), "exit"); e == nil || calls != 0 {
		t.Fatal("ran against inactive exit")
	}
	m.Close(context.Background())
	source.material.Proxy.Password = "wrong"
	m, _ = New(context.Background(), source, exitFixture{context.Background(), true}, options)
	if _, e := m.Start(context.Background(), "exit"); e == nil || calls != 0 {
		t.Fatal("ran with mismatching recipient")
	}
	if _, ok := m.Snapshot("exit"); ok {
		t.Fatal("failed start left fake runtime")
	}
	m.Close(context.Background())
}
