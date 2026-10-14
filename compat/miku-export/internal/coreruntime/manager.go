// Package coreruntime owns only Miku-generated child cores. It never controls
// existing system services or executes raw browser-supplied core configurations.
package coreruntime

import (
	"context"
	"errors"
	"sync"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/ingress"
	"mikugate/internal/security"
)

var ErrBusy = errors.New("受管入口正在运行或操作中，请先停止")
var ErrUnavailable = errors.New("受管核心未由管理员配置，或当前平台不支持")
var ErrCore = errors.New("受管核心配置校验/启动/监听归属未通过；未应用到已有服务")
var ErrExit = errors.New("逻辑出口必须已运行且健康，不能把未启动出口当可用")
var ErrClients = errors.New("入口没有有效客户端凭据，请先创建设备")

type State struct {
	DiagnosticCategories []string  `json:"diagnostic_categories,omitempty"`
	ExitID               string    `json:"exit_id"`
	Phase                string    `json:"phase"`
	Core                 string    `json:"core,omitempty"`
	Listen               string    `json:"listen,omitempty"`
	PID                  int       `json:"pid,omitempty"`
	CoreValidated        bool      `json:"core_validated"`
	ListenerOwned        bool      `json:"listener_owned"`
	DataVerified         bool      `json:"data_verified"`
	StartedAt            time.Time `json:"started_at,omitempty"`
	Reason               string    `json:"reason"`
}
type Source interface {
	Managed(string) (ingress.ManagedMaterial, string, bool, error)
	ExitForRuntime(string) (domain.Exit, string, error)
	AddEvent(string, string, string, string) error
}
type Exits interface {
	WithActive(string, func(context.Context, gateway.State) error) error
}
type Process interface {
	Done() <-chan struct{}
	PID() int
	Stop(context.Context) error
}
type Runner func(context.Context, string, []byte, string) (Process, error)
type Validator func(context.Context, string, []byte) error
type Options struct {
	Runner      Runner
	Validator   Validator
	XrayPath    string
	SingBoxPath string
	MaxActive   int
}
type entry struct {
	cancel        context.CancelFunc
	done          chan struct{}
	process       Process
	state         State
	stopRequested bool
	err           error
}
type Manager struct {
	source    Source
	exits     Exits
	runner    Runner
	validator Validator
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	entries   map[string]*entry
	reserved  map[string]bool
	closed    bool
	limit     int
	checks    map[chan struct{}]bool
}

func New(ctx context.Context, source Source, exits Exits, options Options) (*Manager, error) {
	if source == nil || exits == nil {
		return nil, ErrUnavailable
	}
	if options.MaxActive == 0 {
		options.MaxActive = 3
	}
	if options.MaxActive < 1 || options.MaxActive > 8 {
		return nil, ErrUnavailable
	}
	if options.Runner == nil {
		options.Runner = nativeRunner(options)
	}
	if options.Validator == nil {
		options.Validator = nativeValidator(options)
	}
	life, cancel := context.WithCancel(ctx)
	return &Manager{source: source, exits: exits, runner: options.Runner, validator: options.Validator, ctx: life, cancel: cancel, entries: map[string]*entry{}, reserved: map[string]bool{}, checks: map[chan struct{}]bool{}, limit: options.MaxActive}, nil
}

// ValidateConfig performs a bounded official check only, without starting a
// listener. Callers must supply internally generated material and hold the
// stopped-operation reservation before replacing durable config.
func (m *Manager) ValidateConfig(ctx context.Context, core string, raw []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if (core != "xray" && core != "sing-box") || len(raw) == 0 || len(raw) > 1<<20 {
		return ErrCore
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	if len(m.entries)+len(m.checks) >= m.limit {
		m.mu.Unlock()
		return ErrBusy
	}
	done := make(chan struct{})
	m.checks[done] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.checks, done); close(done); m.mu.Unlock() }()
	bounded, cancel := context.WithTimeout(ctx, 6*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := m.validator(bounded, core, raw); err != nil {
		return err
	}
	return bounded.Err()
}
func (m *Manager) WithStopped(id string, fn func() error) error {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	if m.entries[id] != nil || m.reserved[id] {
		m.mu.Unlock()
		return ErrBusy
	}
	m.reserved[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.reserved, id); m.mu.Unlock() }()
	return fn()
}
func (m *Manager) Snapshot(id string) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	if e == nil {
		return State{}, false
	}
	state := e.state
	if diagnostics, ok := e.process.(interface{ DiagnosticCategories() []string }); ok {
		state.DiagnosticCategories = diagnostics.DiagnosticCategories()
	}
	return state, true
}
func (m *Manager) Start(ctx context.Context, id string) (State, error) {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return State{}, ErrUnavailable
	}
	if m.entries[id] != nil || m.reserved[id] || len(m.entries)+len(m.checks) >= m.limit {
		m.mu.Unlock()
		return State{}, ErrBusy
	}
	life, cancel := context.WithCancel(m.ctx)
	e := &entry{cancel: cancel, done: make(chan struct{}), state: State{ExitID: id, Phase: "starting", Reason: "核验保存素材、活动出口和实际核心"}}
	m.entries[id] = e
	m.mu.Unlock()
	stopRequest := context.AfterFunc(ctx, cancel)
	defer stopRequest()
	err := m.exits.WithActive(id, func(exitLife context.Context, exitState gateway.State) error {
		if !exitState.Healthy || !exitState.ProxyListening {
			return ErrExit
		}
		stopExit := context.AfterFunc(exitLife, cancel)
		material, _, ok, err := m.source.Managed(id)
		if err != nil || !ok {
			stopExit()
			if err != nil {
				return err
			}
			return ErrUnavailable
		}
		exit, hash, err := m.source.ExitForRuntime(id)
		if err != nil {
			stopExit()
			return err
		}
		if material.Proxy.Port != exit.Port || material.Proxy.Username != exit.Username || !security.VerifyPassword(hash, material.Proxy.Password) {
			stopExit()
			return ErrCore
		}
		if material.ActiveClientCount() == 0 {
			stopExit()
			return ErrClients
		}
		bundle, err := material.Render()
		if err != nil {
			stopExit()
			return err
		}
		listen := joinListen(material.Request)
		m.mu.Lock()
		e.state.Core = material.Request.Core
		e.state.Listen = listen
		m.mu.Unlock()
		process, err := m.runner(life, material.Request.Core, bundle.ServerJSON, listen)
		if err != nil {
			stopExit()
			return err
		}
		if life.Err() != nil {
			process.Stop(context.Background())
			stopExit()
			return ErrCore
		}
		m.mu.Lock()
		e.process = process
		e.state.Phase = "running"
		e.state.PID = process.PID()
		e.state.CoreValidated = true
		e.state.ListenerOwned = true
		e.state.StartedAt = time.Now().UTC()
		e.state.Reason = "真实核心已校验并持有监听；尚未逐条验证客户端业务握手"
		m.mu.Unlock()
		go func() {
			defer stopExit()
			select {
			case <-process.Done():
			case <-life.Done():
			}
			m.finish(id, e)
		}()
		return nil
	})
	if err != nil {
		cancel()
		m.mu.Lock()
		if m.entries[id] == e {
			delete(m.entries, id)
		}
		m.mu.Unlock()
		close(e.done)
		return State{}, err
	}
	state, ok := m.Snapshot(id)
	if !ok {
		return State{}, ErrCore
	}
	m.source.AddEvent("info", "ingress.started", "独立受管核心启动并核验监听归属，不控制已有面板", id)
	return state, nil
}
func (m *Manager) finish(id string, e *entry) {
	e.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e.process != nil {
		e.err = e.process.Stop(ctx)
	}
	m.mu.Lock()
	if e.err == nil {
		if m.entries[id] == e {
			delete(m.entries, id)
		}
	} else {
		e.state.Phase = "cleanup-required"
		e.state.Reason = "核心停止未确认，不报告已清理"
	}
	m.mu.Unlock()
	m.source.AddEvent("info", "ingress.stopped", "受管核心租约或进程结束；不回退直连", id)
	close(e.done)
}
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	if m.reserved[id] {
		m.mu.Unlock()
		return ErrBusy
	}
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		return nil
	}
	e.stopRequested = true
	e.state.Phase = "stopping"
	e.cancel()
	m.mu.Unlock()
	select {
	case <-e.done:
		return e.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	all := []*entry{}
	for _, e := range m.entries {
		all = append(all, e)
	}
	checks := make([]chan struct{}, 0, len(m.checks))
	for done := range m.checks {
		checks = append(checks, done)
	}
	m.mu.Unlock()
	for _, done := range checks {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, e := range all {
		select {
		case <-e.done:
			if e.err != nil {
				return e.err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
