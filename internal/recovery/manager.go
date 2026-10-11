// Package recovery coordinates bounded fail-closed reconnects; no direct fallback.
package recovery

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"sync"
	"time"
)

var ErrBusy = errors.New("自动恢复正在切换或停止，等待操作完成")

type Source interface {
	ListExits() ([]domain.Exit, error)
	Recovery(string) (domain.RecoveryPolicy, error)
	PauseRecovery(string) error
	AddEvent(string, string, string, string) error
}
type Runtime interface {
	Snapshot(string) (gateway.State, bool)
	Start(context.Context, string) (gateway.State, error)
	Stop(context.Context, string) error
}
type Status struct {
	Enabled          bool      `json:"enabled"`
	Phase            string    `json:"phase"`
	Attempts         int       `json:"attempts"`
	NextAttempt      time.Time `json:"next_attempt,omitempty"`
	Reason           string    `json:"reason"`
	IngressResumed   bool      `json:"ingress_resumed"`
	IngressReason    string    `json:"ingress_reason,omitempty"`
	CurrentCandidate string    `json:"current_candidate,omitempty"`
}
type task struct {
	slot   chan struct{}
	cancel context.CancelFunc
	status Status
	cursor int
}
type Options struct {
	Interval      time.Duration
	RetryMin      time.Duration
	RetryMax      time.Duration
	Prepare       func(context.Context, domain.Exit, domain.RecoveryPolicy, string) error
	StopIngress   func(context.Context, string) error
	ResumeIngress func(context.Context, string) error
}
type Manager struct {
	source  Source
	runtime Runtime
	options Options
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	tasks   map[string]*task
}

func New(ctx context.Context, s Source, r Runtime, o Options) *Manager {
	if o.Interval == 0 {
		o.Interval = 5 * time.Second
	}
	if o.RetryMin == 0 {
		o.RetryMin = 30 * time.Second
	}
	if o.RetryMax == 0 {
		o.RetryMax = 15 * time.Minute
	}
	life, cancel := context.WithCancel(ctx)
	m := &Manager{s, r, o, life, cancel, make(chan struct{}), sync.Mutex{}, map[string]*task{}}
	go m.loop()
	return m
}
func (m *Manager) task(id string) *task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tasks[id] == nil {
		m.tasks[id] = &task{slot: make(chan struct{}, 1), status: Status{Phase: "idle", Reason: "自动恢复默认关闭"}}
	}
	return m.tasks[id]
}
func (m *Manager) Control(ctx context.Context, id string, fn func() error) error {
	t := m.task(id)
	select {
	case t.slot <- struct{}{}:
		defer func() { <-t.slot }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Pause(id string) error {
	if e := m.source.PauseRecovery(id); e != nil {
		return e
	}
	t := m.task(id)
	m.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.status = Status{Phase: "paused", Reason: "用户停止已持久取消自动恢复，不会后台重新启动"}
	m.mu.Unlock()
	return nil
}
func (m *Manager) Wake(id string) {
	t := m.task(id)
	m.mu.Lock()
	t.status.NextAttempt = time.Time{}
	t.status.Attempts = 0
	m.mu.Unlock()
}
func (m *Manager) Status(id string) Status {
	t := m.task(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.status
}
func (m *Manager) Close() { m.cancel(); <-m.done }
func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(m.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.tick()
		}
	}
}
func (m *Manager) tick() {
	exits, e := m.source.ListExits()
	if e != nil {
		return
	}
	present := map[string]bool{}
	for _, exit := range exits {
		present[exit.ID] = true
	}
	m.mu.Lock()
	for id := range m.tasks {
		if !present[id] {
			delete(m.tasks, id)
		}
	}
	m.mu.Unlock()
	for _, exit := range exits {
		if m.ctx.Err() != nil {
			return
		}
		p, e := m.source.Recovery(exit.ID)
		if e != nil || !p.Enabled {
			continue
		}
		t := m.task(exit.ID)
		state, active := m.runtime.Snapshot(exit.ID)
		if active && state.Healthy && state.ProxyListening {
			m.mu.Lock()
			t.status.Enabled = true
			t.status.Phase = "healthy"
			t.status.Reason = "当前实测健康，无候选联网或换线"
			t.status.Attempts = 0
			t.status.NextAttempt = time.Time{}
			m.mu.Unlock()
			continue
		}
		if active && (state.Phase == "starting" || state.Phase == "stopping") {
			continue
		}
		m.mu.Lock()
		next := t.status.NextAttempt
		m.mu.Unlock()
		if time.Now().Before(next) {
			continue
		}
		select {
		case t.slot <- struct{}{}:
		default:
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		m.mu.Lock()
		t.cancel = cancel
		t.status.Enabled = true
		t.status.Phase = "recovering"
		t.status.Reason = "先撤销入口/出口，再验证指定上游；没有直连后备"
		m.mu.Unlock()
		err := m.recover(ctx, exit, p, t)
		cancel()
		m.mu.Lock()
		t.cancel = nil
		current, e := m.source.Recovery(exit.ID)
		if e == nil && !current.Enabled {
			t.status.Enabled = false
			t.status.Phase = "paused"
			t.status.Reason = "用户已暂停自动恢复"
		} else if err != nil {
			t.status.Attempts++
			delay := m.options.RetryMin
			for i := 1; i < t.status.Attempts && delay < m.options.RetryMax; i++ {
				delay *= 2
			}
			if delay > m.options.RetryMax {
				delay = m.options.RetryMax
			}
			t.status.Phase = "backoff"
			t.status.NextAttempt = time.Now().Add(delay)
			t.status.Reason = "当前尝试失败，入口不直连；退避后最多再试一个候选"
		} else {
			t.status.Phase = "healthy"
			t.status.Reason = "实际出口探测/监听已通过，入口身份未改变"
			t.status.Attempts = 0
			t.status.NextAttempt = time.Time{}
		}
		m.mu.Unlock()
		<-t.slot
	}
}
func (m *Manager) recover(ctx context.Context, exit domain.Exit, p domain.RecoveryPolicy, t *task) error {
	if m.options.StopIngress != nil {
		if e := m.options.StopIngress(ctx, exit.ID); e != nil {
			return e
		}
	}
	if e := m.runtime.Stop(ctx, exit.ID); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, e := m.source.Recovery(exit.ID)
	if e != nil || !current.Enabled {
		return context.Canceled
	}
	candidate := ""
	if len(p.NodeIDs) > 0 {
		m.mu.Lock()
		candidate = p.NodeIDs[t.cursor%len(p.NodeIDs)]
		t.cursor++
		t.status.CurrentCandidate = candidate
		m.mu.Unlock()
	}
	if m.options.Prepare != nil {
		if e = m.options.Prepare(ctx, exit, p, candidate); e != nil {
			return e
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state, e := m.runtime.Start(ctx, exit.ID)
	if e != nil {
		return e
	}
	if !state.Healthy || !state.ProxyListening {
		return gateway.ErrProof
	}
	if p.ResumeIngress && m.options.ResumeIngress != nil {
		e = m.options.ResumeIngress(ctx, exit.ID)
		m.mu.Lock()
		t.status.IngressResumed = e == nil
		t.status.IngressReason = ""
		if e != nil {
			t.status.IngressReason = "出口恢复，受管入口未通过资产/核心验证，仍保持停止"
		}
		m.mu.Unlock()
		if e != nil {
			m.source.AddEvent("warning", "exit.ingress_resume_failed", "出口恢复，但受管入口资产/核心未验证；入口保持停止，需重新检查", exit.ID)
		}
	}
	m.source.AddEvent("info", "exit.auto_recovered", "自动恢复已验证出口DNS/TLS/监听；凭据不变，不代表住宅或纯净", exit.ID)
	return nil
}
