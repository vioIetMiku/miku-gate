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
	MaxConcurrent int
	Prepare       func(context.Context, domain.Exit, domain.RecoveryPolicy, string) error
	StopIngress   func(context.Context, string) error
	ResumeIngress func(context.Context, string) error
}
type Manager struct {
	source   Source
	runtime  Runtime
	options  Options
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	tasks    map[string]*task
	workers  sync.WaitGroup
	capacity chan struct{}
	closed   bool
	cursor   int
}

func New(ctx context.Context, s Source, r Runtime, o Options) *Manager {
	if o.Interval <= 0 {
		o.Interval = 5 * time.Second
	}
	if o.RetryMin <= 0 {
		o.RetryMin = 30 * time.Second
	}
	if o.RetryMax <= 0 {
		o.RetryMax = 15 * time.Minute
	}
	if o.RetryMax < o.RetryMin {
		o.RetryMax = o.RetryMin
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = 3
	}
	if o.MaxConcurrent > 16 {
		o.MaxConcurrent = 16
	}
	life, cancel := context.WithCancel(ctx)
	m := &Manager{source: s, runtime: r, options: o, ctx: life, cancel: cancel, done: make(chan struct{}), tasks: map[string]*task{}, capacity: make(chan struct{}, o.MaxConcurrent)}
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
		// Cancellation can race with the ready slot; never invoke a late mutation.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.ctx.Err() != nil {
			return m.ctx.Err()
		}
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}
func (m *Manager) Pause(id string) error {
	if e := m.source.PauseRecovery(id); e != nil {
		return e
	}
	t := m.task(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	t.status = Status{Phase: "paused", Reason: "用户停止已持久取消自动恢复，不会后台重新启动"}
	return nil
}

// CancelAttempt interrupts only the current attempt. Persistent policy remains
// under explicit store control, so stopping an ingress can keep exit recovery.
func (m *Manager) CancelAttempt(id string) {
	t := m.task(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
}
func (m *Manager) MaxConcurrent() int { return cap(m.capacity) }
func (m *Manager) Wake(id string) {
	t := m.task(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	t.status.NextAttempt = time.Time{}
	t.status.Attempts = 0
}
func (m *Manager) Status(id string) Status {
	t := m.task(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.status
}
func (m *Manager) Close() {
	// Serialize worker registration with shutdown; Wait never races a late Add.
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	<-m.done
	m.workers.Wait()
}
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
	if e != nil || m.ctx.Err() != nil {
		return
	}
	present := map[string]bool{}
	for _, exit := range exits {
		present[exit.ID] = true
	}
	m.mu.Lock()
	for id, t := range m.tasks {
		if !present[id] {
			if t.cancel != nil {
				t.cancel()
			}
			if len(t.slot) == 0 {
				delete(m.tasks, id)
			}
		}
	}
	start := 0
	if len(exits) > 0 {
		start = m.cursor % len(exits)
		m.cursor = (start + 1) % len(exits)
	}
	m.mu.Unlock()
	// Round robin visits every logical exit; a slow attempt runs outside this loop.
	for offset := 0; offset < len(exits); offset++ {
		if m.ctx.Err() != nil {
			return
		}
		exit := exits[(start+offset)%len(exits)]
		p, e := m.source.Recovery(exit.ID)
		t := m.task(exit.ID)
		m.mu.Lock()
		if e != nil || !p.Enabled {
			if t.cancel != nil {
				t.cancel()
			}
			if e == nil && t.status.Enabled {
				t.status.Enabled = false
				t.status.Phase = "paused"
				t.status.Reason = "自动恢复已关闭"
			}
			m.mu.Unlock()
			continue
		}
		busy := t.cancel != nil
		m.mu.Unlock()
		if busy {
			continue
		}
		// Share the per-exit gate with all user configuration/lifecycle operations.
		select {
		case t.slot <- struct{}{}:
		default:
			continue
		}
		state, active := m.runtime.Snapshot(exit.ID)
		m.mu.Lock()
		if active && state.Healthy && state.ProxyListening {
			t.status.Enabled = true
			t.status.Phase = "healthy"
			t.status.Reason = "当前实测健康，无候选联网或换线"
			t.status.Attempts = 0
			t.status.NextAttempt = time.Time{}
			m.mu.Unlock()
			<-t.slot
			continue
		}
		if active && (state.Phase == "starting" || state.Phase == "stopping") || time.Now().Before(t.status.NextAttempt) || m.closed || m.ctx.Err() != nil {
			m.mu.Unlock()
			<-t.slot
			continue
		}
		select {
		case m.capacity <- struct{}{}:
		default:
			t.status.Enabled = true
			t.status.Phase = "waiting"
			t.status.Reason = "恢复并发预算已占用；等待可用名额，不联网探测候选"
			m.mu.Unlock()
			<-t.slot
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		t.cancel = cancel
		t.status.Enabled = true
		t.status.Phase = "recovering"
		t.status.Reason = "先撤销入口/出口，再验证指定上游；没有直连后备"
		t.status.IngressResumed = false
		t.status.IngressReason = ""
		t.status.CurrentCandidate = ""
		m.workers.Add(1)
		m.mu.Unlock()
		go m.attempt(ctx, cancel, exit, p, t)
	}
}
func retryDelay(min, max time.Duration, attempt int) time.Duration {
	delay := min
	for i := 1; i < attempt && delay < max; i++ {
		if delay >= max-delay {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}
func (m *Manager) attempt(ctx context.Context, cancel context.CancelFunc, exit domain.Exit, p domain.RecoveryPolicy, t *task) {
	defer m.workers.Done()
	defer func() { <-m.capacity; <-t.slot }()
	err := m.recover(ctx, exit, p, t)
	cancel()
	current, policyErr := m.source.Recovery(exit.ID)
	m.mu.Lock()
	defer m.mu.Unlock()
	t.cancel = nil
	if m.closed || m.ctx.Err() != nil {
		t.status.Enabled = false
		t.status.Phase = "stopped"
		t.status.NextAttempt = time.Time{}
		t.status.Reason = "控制服务关闭，恢复任务已取消；停止异常需按审计检查"
	} else if policyErr == nil && !current.Enabled {
		t.status.Enabled = false
		t.status.Phase = "paused"
		t.status.NextAttempt = time.Time{}
		t.status.Reason = "用户已暂停自动恢复"
	} else if err != nil || policyErr != nil {
		t.status.Attempts++
		t.status.Phase = "backoff"
		t.status.NextAttempt = time.Now().Add(retryDelay(m.options.RetryMin, m.options.RetryMax, t.status.Attempts))
		t.status.Reason = "当前尝试失败，入口不直连；退避后最多再试一个候选"
	} else {
		t.status.Phase = "healthy"
		t.status.Reason = "实际出口探测/监听已通过，入口身份未改变"
		t.status.Attempts = 0
		t.status.NextAttempt = time.Time{}
	}
}
func (m *Manager) allowed(ctx context.Context, id string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p, e := m.source.Recovery(id)
	if e != nil {
		return e
	}
	if !p.Enabled {
		return context.Canceled
	}
	return nil
}
func (m *Manager) cleanup(id string) error {
	// A canceled start may already have opened a listener. Close using a separate
	// bounded context; never reuse the canceled attempt for fail-closed cleanup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proxyErr := m.runtime.Stop(ctx, id)
	var ingressErr error
	if m.options.StopIngress != nil {
		ingressErr = m.options.StopIngress(ctx, id)
	}
	return errors.Join(proxyErr, ingressErr)
}
func (m *Manager) recover(ctx context.Context, exit domain.Exit, p domain.RecoveryPolicy, t *task) (err error) {
	var ingressErr error
	if m.options.StopIngress != nil {
		ingressErr = m.options.StopIngress(ctx, exit.ID)
	}
	// Even an ingress-stop failure must not leave the logical proxy forwarding.
	if e := errors.Join(ingressErr, m.runtime.Stop(ctx, exit.ID)); e != nil {
		return e
	}
	if e := m.allowed(ctx, exit.ID); e != nil {
		return e
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
		if e := m.options.Prepare(ctx, exit, p, candidate); e != nil {
			return e
		}
	}
	if e := m.allowed(ctx, exit.ID); e != nil {
		return e
	}
	// Stop all partial starts/proof failures and late cancellation before returning.
	defer func() {
		if err != nil {
			if e := m.cleanup(exit.ID); e != nil {
				m.source.AddEvent("error", "exit.recovery_cleanup_failed", "取消/失败后的停止未全部确认；不得视为健康，需检查运行时", exit.ID)
			}
		}
	}()
	state, e := m.runtime.Start(ctx, exit.ID)
	if e != nil {
		return e
	}
	if !state.Healthy || !state.ProxyListening {
		return gateway.ErrProof
	}
	if e = m.allowed(ctx, exit.ID); e != nil {
		return e
	}
	current, policyErr := m.source.Recovery(exit.ID)
	if policyErr != nil {
		return policyErr
	}
	if current.ResumeIngress && m.options.ResumeIngress != nil {
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
	if e = m.allowed(ctx, exit.ID); e != nil {
		return e
	}
	m.source.AddEvent("info", "exit.auto_recovered", "自动恢复已验证出口DNS/TLS/监听；凭据不变，不代表住宅或纯净", exit.ID)
	return nil
}
