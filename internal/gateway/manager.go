// Package gateway coordinates real authenticated listeners and outbound sessions.
// Durable configuration is not proof of liveness: active state exists only here.
package gateway

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/proxy"
	"mikugate/internal/upstream"
	"mikugate/internal/vpnipc"
)

var ErrBusy = errors.New("出口正在运行或有操作进行中，请先停止并等待完成")
var ErrCapacity = errors.New("已达到同时运行出口上限，请先停止其他出口")
var ErrUnavailable = errors.New("尚未配置代理上游或OpenVPN节点；请先选择出口")
var ErrPolicy = errors.New("没有可核验的住宅属性依据，不能启动严格住宅策略")
var ErrClosed = errors.New("代理运行时正在关闭")
var ErrListen = errors.New("代理入口端口不可用；不会关闭或抢占其他服务")
var ErrProof = errors.New("上游未提供可核验的DNS与代理路径")

type Proof struct {
	Kind           string `json:"kind"`
	ProtectionMode string `json:"protection_mode"`
	DNSVerified    bool   `json:"dns_verified"`
	KernelVerified bool   `json:"kernel_verified"`
	ExitIP         string `json:"exit_ip,omitempty"`
}
type State struct {
	ExitID            string    `json:"exit_id"`
	Phase             string    `json:"phase"`
	Listen            string    `json:"listen,omitempty"`
	ProxyListening    bool      `json:"proxy_listening"`
	Healthy           bool      `json:"healthy"`
	Proof             Proof     `json:"proof"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	CheckedAt         time.Time `json:"checked_at,omitempty"`
	CheckMilliseconds int64     `json:"check_ms"`
	Reason            string    `json:"reason"`
}
type Session interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	Probe(context.Context) (Proof, error)
	Close() error
}

// A factory may later return a verified OpenVPN worker session instead of a
// proxy session. The listener, credentials and lifecycle do not depend on it.
type Factory func(context.Context, domain.Exit, *domain.UpstreamConfig) (Session, error)
type Source interface {
	ExitForRuntime(string) (domain.Exit, string, error)
	Upstream(string) (domain.UpstreamConfig, string, bool, error)
	SetStatus(string, string, string) error
	AddEvent(string, string, string, string) error
}
type Options struct {
	Factory        Factory
	MaxExits       int
	MaxConnections int
	HealthInterval time.Duration
	Listen         func(context.Context, string, string) (net.Listener, error)
}
type entry struct {
	closeOnce     sync.Once
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	slot          chan struct{}
	state         State
	session       Session
	stopRequested bool
	finalErr      error
	closeErr      error
}
type Manager struct {
	source    Source
	options   Options
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	entries   map[string]*entry
	reserved  map[string]bool
	authSlots chan struct{}
	closed    bool
}

func New(ctx context.Context, source Source, options Options) (*Manager, error) {
	if source == nil {
		return nil, errors.New("runtime configuration source is required")
	}
	if options.Factory == nil {
		options.Factory = proxyFactory
	}
	if options.MaxExits == 0 {
		options.MaxExits = 3
	}
	if options.MaxConnections == 0 {
		options.MaxConnections = 64
	}
	if options.HealthInterval == 0 {
		options.HealthInterval = 30 * time.Second
	}
	if options.MaxExits < 1 || options.MaxExits > 8 || options.MaxConnections < 1 || options.MaxConnections > 128 || options.HealthInterval < 10*time.Millisecond {
		return nil, errors.New("invalid runtime limits")
	}
	if options.Listen == nil {
		options.Listen = (&net.ListenConfig{}).Listen
	}
	life, cancel := context.WithCancel(ctx)
	return &Manager{source: source, options: options, ctx: life, cancel: cancel, entries: map[string]*entry{}, reserved: map[string]bool{}, authSlots: make(chan struct{}, 1)}, nil
}

type proxySession struct {
	*upstream.Client
	kind string
}

func (s *proxySession) Probe(ctx context.Context) (Proof, error) {
	if err := s.Client.Probe(ctx); err != nil {
		return Proof{}, err
	}
	return Proof{Kind: s.kind, ProtectionMode: "proxy_only", DNSVerified: true}, nil
}
func proxyFactory(ctx context.Context, _ domain.Exit, config *domain.UpstreamConfig) (Session, error) {
	if config == nil {
		return nil, ErrUnavailable
	}
	client, err := upstream.New(ctx, *config)
	if err != nil {
		return nil, err
	}
	return &proxySession{Client: client, kind: config.Kind}, nil
}
func validProof(proof Proof) bool {
	if !proof.DNSVerified {
		return false
	}
	if proof.ProtectionMode == "proxy_only" {
		return !proof.KernelVerified && (proof.Kind == "socks5" || proof.Kind == "http" || proof.Kind == "https")
	}
	return proof.ProtectionMode == "namespace" && proof.Kind == "openvpn" && proof.KernelVerified
}
func (m *Manager) Snapshot(id string) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return State{}, false
	}
	return e.state, true
}
func (m *Manager) States() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]State, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e.state)
	}
	return out
}

// WithStopped reserves one exit without holding a global mutex across filesystem
// or IPC work. Start/delete/configure/credential changes cannot race for that exit.
func (m *Manager) WithStopped(id string, operation func() error) error {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.entries[id] != nil || m.reserved[id] {
		m.mu.Unlock()
		return ErrBusy
	}
	m.reserved[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.reserved, id); m.mu.Unlock() }()
	return operation()
}
func (m *Manager) Start(ctx context.Context, id string) (State, error) {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return State{}, ErrClosed
	}
	if m.reserved[id] {
		m.mu.Unlock()
		return State{}, ErrBusy
	}
	if current := m.entries[id]; current != nil {
		state := current.state
		m.mu.Unlock()
		if state.Phase == "running" || state.Phase == "degraded" {
			return state, nil
		}
		return state, ErrBusy
	}
	if len(m.entries) >= m.options.MaxExits {
		m.mu.Unlock()
		return State{}, ErrCapacity
	}
	life, cancel := context.WithCancel(m.ctx)
	e := &entry{ctx: life, cancel: cancel, done: make(chan struct{}), slot: make(chan struct{}, 1), state: State{ExitID: id, Phase: "starting", Reason: "正在校验代理上游和经上游的DNS；尚未开放入口"}}
	m.entries[id] = e
	m.mu.Unlock()
	startup, cancelStartup := context.WithTimeout(life, 35*time.Second)
	stopRequest := context.AfterFunc(ctx, cancel)
	stopStartup := context.AfterFunc(startup, cancel)
	// Once startup returns successfully, HTTP request cancellation must not kill
	// the established runtime. The manager/exit context owns its lifetime instead.
	defer func() { stopRequest(); stopStartup(); cancelStartup() }()
	fail := func(err error) (State, error) { m.finish(e, err); return State{}, err }
	exit, hash, err := m.source.ExitForRuntime(id)
	if err != nil {
		return fail(err)
	}
	if exit.Listen != "127.0.0.1" || exit.Port < 1024 || exit.Port > 65535 {
		return fail(errors.New("代理入口必须是本机非特权端口"))
	}
	if exit.Policy == "residential_strict" {
		return fail(ErrPolicy)
	}
	config, _, exists, err := m.source.Upstream(id)
	if err != nil {
		return fail(err)
	}
	var selected *domain.UpstreamConfig
	if exists {
		selected = &config
		if config.Host == "127.0.0.1" && config.Port == exit.Port {
			return fail(errors.New("代理上游不能指向自己的入口"))
		}
	}
	authenticate, err := newAuthenticator(exit.Username, hash, m.authSlots)
	if err != nil {
		return fail(err)
	}
	session, err := m.options.Factory(life, exit, selected)
	if session != nil {
		e.session = session
	}
	if err != nil {
		return fail(err)
	}
	if session == nil {
		return fail(ErrUnavailable)
	}
	e.session = session
	started := time.Now()
	proof, err := session.Probe(startup)
	if err == nil && !validProof(proof) {
		err = ErrProof
	}
	if err != nil {
		return fail(err)
	}
	listener, err := m.options.Listen(startup, "tcp4", net.JoinHostPort(exit.Listen, strconv.Itoa(exit.Port)))
	if err != nil {
		return fail(errors.Join(ErrListen, err))
	}
	if startup.Err() != nil {
		listener.Close()
		return fail(startup.Err())
	}
	if err = m.source.AddEvent("info", "exit.proxy_prepared", "代理通路已通过校验且入口端口已绑定；以实时状态确认最终启动结果，不代表住宅或公网出口IP已核验", id); err != nil {
		listener.Close()
		return fail(err)
	}
	m.mu.Lock()
	if !stopRequest() || !stopStartup() || ctx.Err() != nil || startup.Err() != nil || life.Err() != nil {
		m.mu.Unlock()
		listener.Close()
		cause := startup.Err()
		if cause == nil {
			cause = context.Canceled
		}
		return fail(cause)
	}
	e.state = State{ExitID: id, Phase: "running", Listen: listener.Addr().String(), ProxyListening: true, Healthy: true, Proof: proof, StartedAt: time.Now().UTC(), CheckedAt: time.Now().UTC(), CheckMilliseconds: time.Since(started).Milliseconds(), Reason: "本机认证代理运行中；业务DNS与TCP只经指定上游，未核验住宅属性或公网出口IP"}
	if proof.Kind == "openvpn" {
		e.state.Reason = "独立OpenVPN与TUN绑定worker已验证；TLS/DNS和出口IP通过，不代表住宅或节点信誉"
	}
	state := e.state
	m.mu.Unlock()
	go m.serve(e, listener, authenticate)
	return state, nil
}
func (m *Manager) serve(e *entry, listener net.Listener, authenticate func(string, string) bool) {
	if session, ok := e.session.(interface{ Done() <-chan struct{} }); ok {
		go func() {
			select {
			case <-session.Done():
				e.cancel()
			case <-e.ctx.Done():
			}
		}()
	}
	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		ticker := time.NewTicker(m.options.HealthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
				m.probe(e.ctx, e)
			}
		}
	}()
	server := proxy.Server{Dial: e.session.DialContext, Authenticate: authenticate, MaxConnections: m.options.MaxConnections}
	err := server.Serve(e.ctx, listener)
	e.cancel()
	m.closeSession(e)
	<-healthDone
	m.finish(e, err)
}
func (m *Manager) closeSession(e *entry) {
	e.closeOnce.Do(func() {
		if e.session != nil {
			e.closeErr = e.session.Close()
		}
	})
}
func (m *Manager) finish(e *entry, cause error) {
	e.cancel()
	m.closeSession(e)
	m.mu.Lock()
	intentional := e.stopRequested || m.ctx.Err() != nil || errors.Is(cause, context.Canceled)
	e.state.Phase = "stopping"
	e.state.ProxyListening = false
	e.state.Healthy = false
	m.mu.Unlock()
	status, reason := "blocked", Reason(cause)
	if intentional {
		status, reason = "stopped", "代理入口、在途连接与上游会话均已停止"
	}
	if cause == nil && !intentional {
		reason = "代理监听意外结束，入口已关闭"
	}
	if e.closeErr != nil {
		status, reason = "blocked", "执行器未确认会话已停止；本机入口已关闭，请检查独立执行器状态"
	}
	var recordErr error
	if !errors.Is(cause, domain.ErrNotFound) {
		recordErr = m.source.SetStatus(e.state.ExitID, status, reason)
	}
	m.mu.Lock()
	e.finalErr = errors.Join(recordErr, e.closeErr)
	if m.entries[e.state.ExitID] == e {
		delete(m.entries, e.state.ExitID)
	}
	m.mu.Unlock()
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
		return m.source.SetStatus(id, "stopped", "没有活动代理会话；出口已停止")
	}
	e.stopRequested = true
	e.state.Phase = "stopping"
	e.state.Healthy = false
	e.state.Reason = "正在关闭代理入口与所有在途连接"
	e.cancel()
	m.mu.Unlock()
	select {
	case <-e.done:
		return e.finalErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Check(ctx context.Context, id string) (State, error) {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		if _, _, err := m.source.ExitForRuntime(id); err != nil {
			return State{}, err
		}
		return State{}, ErrUnavailable
	}
	if e.state.Phase != "running" && e.state.Phase != "degraded" {
		m.mu.Unlock()
		return State{}, ErrBusy
	}
	m.mu.Unlock()
	err := m.probe(ctx, e)
	state, exists := m.Snapshot(id)
	if !exists {
		return State{ExitID: id, Phase: "stopped"}, ErrClosed
	}
	return state, err
}
func (m *Manager) probe(ctx context.Context, e *entry) error {
	select {
	case e.slot <- struct{}{}:
		defer func() { <-e.slot }()
	default:
		return ErrBusy
	}
	bounded, cancel := context.WithTimeout(ctx, 6*time.Second)
	stop := context.AfterFunc(e.ctx, cancel)
	defer func() { stop(); cancel() }()
	now := time.Now()
	proof, err := e.session.Probe(bounded)
	if err == nil && !validProof(proof) {
		err = ErrProof
	}
	m.mu.Lock()
	if m.entries[e.state.ExitID] != e || e.stopRequested || e.ctx.Err() != nil {
		m.mu.Unlock()
		return err
	}
	wasHealthy := e.state.Healthy
	e.state.CheckedAt = time.Now().UTC()
	e.state.CheckMilliseconds = time.Since(now).Milliseconds()
	e.state.Healthy = err == nil
	if err == nil {
		e.state.Proof = proof
		e.state.Phase = "running"
		e.state.Reason = "上游与业务DNS探测通过；入口保持运行"
	} else {
		e.state.Proof.DNSVerified = false
		e.state.Proof.KernelVerified = false
		e.state.Phase = "degraded"
		e.state.Reason = Reason(err)
	}
	id := e.state.ExitID
	m.mu.Unlock()
	if wasHealthy != (err == nil) && e.ctx.Err() == nil {
		level, kind := "info", "exit.upstream_recovered"
		if err != nil {
			level, kind = "warning", "exit.upstream_unavailable"
		}
		m.source.AddEvent(level, kind, "代理上游健康状态发生变化；不使用宿主直连后备", id)
	}
	return err
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	all := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		e.stopRequested = true
		all = append(all, e)
	}
	m.cancel()
	m.mu.Unlock()
	var result error
	for _, e := range all {
		select {
		case <-e.done:
			result = errors.Join(result, e.finalErr)
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
	}
	return result
}
func Reason(err error) string {
	switch {
	case err == nil:
		return "代理未运行"
	case errors.Is(err, ErrVPNDisabled):
		return ErrVPNDisabled.Error()
	case errors.Is(err, vpnipc.ErrProfile):
		return "VPN配置或预期服务器身份不符合策略；未放宽TLS校验"
	case errors.Is(err, vpnipc.ErrUnavailable):
		return "隔离OpenVPN执行器或会话不可用；未使用宿主路由后备"
	case errors.Is(err, vpnipc.ErrBusy):
		return "独立VPN执行器已达到会话上限或出口正在操作中"
	case errors.Is(err, ErrUnavailable):
		return ErrUnavailable.Error()
	case errors.Is(err, ErrPolicy):
		return ErrPolicy.Error()
	case errors.Is(err, upstream.ErrAuthentication):
		return "代理上游认证失败；未开放或绕过上游"
	case errors.Is(err, upstream.ErrTLS):
		return "上游或DNS服务TLS身份校验失败；未跳过证书校验"
	case errors.Is(err, upstream.ErrDNS):
		return "经代理上游的加密DNS不可用；没有宿主DNS后备"
	case errors.Is(err, upstream.ErrDestination):
		return "目标或DNS答案违反公网地址策略"
	case errors.Is(err, context.Canceled):
		return "启动或探测已取消"
	case errors.Is(err, context.DeadlineExceeded):
		return "上游校验超时；没有开放未经验证的入口"
	case errors.Is(err, ErrListen):
		return ErrListen.Error()
	case errors.Is(err, ErrBusy):
		return ErrBusy.Error()
	case errors.Is(err, ErrCapacity):
		return ErrCapacity.Error()
	}
	return "代理运行失败：请检查上游、端口占用与运行日志；没有直连后备"
}
