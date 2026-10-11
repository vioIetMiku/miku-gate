package recovery

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"sync"
	"testing"
	"time"
)

type source struct {
	mu     sync.Mutex
	policy domain.RecoveryPolicy
	events int
}

func (s *source) ListExits() ([]domain.Exit, error) {
	return []domain.Exit{{ID: "one", Country: "JP"}}, nil
}
func (s *source) Recovery(string) (domain.RecoveryPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy, nil
}
func (s *source) PauseRecovery(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy.Enabled = false
	return nil
}
func (s *source) AddEvent(string, string, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events++
	return nil
}

type runtime struct {
	mu     sync.Mutex
	state  gateway.State
	active bool
	calls  []string
}

func (r *runtime) Snapshot(string) (gateway.State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.active
}
func (r *runtime) Stop(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "stop")
	r.active = false
	return nil
}
func (r *runtime) Start(ctx context.Context, id string) (gateway.State, error) {
	if ctx.Err() != nil {
		return gateway.State{}, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "start")
	r.active = true
	r.state = gateway.State{ExitID: id, Phase: "running", Healthy: true, ProxyListening: true}
	return r.state, nil
}
func TestDisabledHealthySequentialCandidateBackoffAndUserStop(t *testing.T) {
	s := &source{}
	r := &runtime{}
	candidates := []string{}
	m := New(t.Context(), s, r, Options{Interval: time.Hour, Prepare: func(_ context.Context, _ domain.Exit, _ domain.RecoveryPolicy, id string) error {
		candidates = append(candidates, id)
		if id == "first" {
			return errors.New("unverified node")
		}
		return nil
	}})
	defer m.Close()
	m.tick()
	if len(r.calls) != 0 {
		t.Fatal("disabled recovery contacted runtime")
	}
	s.policy = domain.RecoveryPolicy{Enabled: true, NodeIDs: []string{"first", "second"}}
	m.tick()
	if len(candidates) != 1 || candidates[0] != "first" || len(r.calls) != 1 || r.calls[0] != "stop" || m.Status("one").Phase != "backoff" {
		t.Fatal("first failed candidate did not stop/backoff")
	}
	m.tick()
	if len(candidates) != 1 {
		t.Fatal("recovery skipped retry budget")
	}
	task := m.task("one")
	m.mu.Lock()
	task.status.NextAttempt = time.Time{}
	m.mu.Unlock()
	m.tick()
	if len(candidates) != 2 || candidates[1] != "second" || m.Status("one").Phase != "healthy" {
		t.Fatal("second verified reconnect failed")
	}
	before := len(r.calls)
	m.tick()
	if len(r.calls) != before {
		t.Fatal("healthy recovery probed other volunteers")
	}
	if e := m.Pause("one"); e != nil {
		t.Fatal(e)
	}
	r.active = false
	m.tick()
	if len(r.calls) != before {
		t.Fatal("explicit pause restarted")
	}
}
func TestPauseCancelsInFlightPreparationAndNoLateRestart(t *testing.T) {
	s := &source{policy: domain.RecoveryPolicy{Enabled: true}}
	r := &runtime{}
	entered := make(chan struct{})
	m := New(t.Context(), s, r, Options{Interval: time.Hour, Prepare: func(ctx context.Context, _ domain.Exit, _ domain.RecoveryPolicy, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}})
	defer m.Close()
	done := make(chan struct{})
	go func() { m.tick(); close(done) }()
	<-entered
	if e := m.Pause("one"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inflight recovery did not cancel")
	}
	for _, call := range r.calls {
		if call == "start" {
			t.Fatal("stopped recovery restarted later")
		}
	}
	if m.Status("one").Phase != "paused" {
		t.Fatal("pause status false")
	}
}
