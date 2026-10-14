package gateway

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/security"
	"mikugate/internal/vpngate"
	"mikugate/internal/vpnipc"
)

type StandbySource interface {
	VPNSource
	StandbyPolicy(string) (domain.StandbyPolicy, error)
	NodeForRuntime(string) (domain.Node, error)
	NodeBlocked(string) (bool, error)
	AddEvent(string, string, string, string) error
}
type StandbyStatus struct {
	Enabled      bool      `json:"enabled"`
	Ready        bool      `json:"ready"`
	NodeID       string    `json:"node_id,omitempty"`
	ActiveNodeID string    `json:"active_node_id"`
	Promotions   int       `json:"promotions"`
	Reason       string    `json:"reason"`
	CheckedAt    time.Time `json:"checked_at"`
}
type standbySession struct {
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	operation sync.Mutex
	active    Session
	spare     Session
	source    StandbySource
	client    *vpnipc.Client
	exit      domain.Exit
	policy    domain.StandbyPolicy
	primary   domain.VPNConfig
	status    StandbyStatus
	done      chan struct{}
	loopDone  chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
	cleanup   sync.WaitGroup
	closed    bool
	closeErr  error
}

func WithHotStandby(source StandbySource, client *vpnipc.Client, base Factory) Factory {
	return func(ctx context.Context, exit domain.Exit, upstream *domain.UpstreamConfig) (Session, error) {
		p, err := source.StandbyPolicy(exit.ID)
		if err != nil {
			return nil, err
		}
		if !p.Enabled {
			return base(ctx, exit, upstream)
		}
		if upstream != nil || client == nil {
			return nil, ErrVPNDisabled
		}
		config, ok, err := source.VPN(exit.ID)
		if err != nil || !ok {
			return nil, ErrUnavailable
		}
		active, err := base(ctx, exit, upstream)
		if err != nil {
			return active, err
		}
		life, cancel := context.WithCancel(ctx)
		s := &standbySession{ctx: life, cancel: cancel, active: active, source: source, client: client, exit: exit, policy: p, primary: config, done: make(chan struct{}), loopDone: make(chan struct{}), status: StandbyStatus{Enabled: true, ActiveNodeID: config.NodeID, Reason: "主隧道就绪，正在独立验证和连接备用"}}
		go s.watch(active)
		go s.maintain()
		return s, nil
	}
}
func (s *standbySession) StandbyStatus() StandbyStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
func (s *standbySession) Done() <-chan struct{} { return s.done }
func (s *standbySession) fail()                 { s.doneOnce.Do(func() { close(s.done) }); s.cancel() }
func (s *standbySession) watch(session Session) {
	d, ok := session.(interface{ Done() <-chan struct{} })
	if !ok {
		return
	}
	select {
	case <-s.ctx.Done():
		return
	case <-d.Done():
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if _, err := s.promote(ctx, session); err != nil {
		s.fail()
	}
}
func (s *standbySession) maintain() {
	defer close(s.loopDone)
	for {
		if s.ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		needs := s.spare == nil && !s.closed
		target := s.policy.NodeID
		if s.status.ActiveNodeID == target {
			target = s.primary.NodeID
		}
		s.mu.Unlock()
		if needs {
			s.fill(target)
		}
		timer := time.NewTimer(2 * time.Minute)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *standbySession) fill(target string) {
	ctx, cancel := context.WithTimeout(s.ctx, 32*time.Second)
	defer cancel()
	node, err := s.source.NodeForRuntime(target)
	if err == nil {
		blocked, e := s.source.NodeBlocked(target)
		if e != nil || blocked || node.Country != s.exit.Country || node.Source != vpngate.Source || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
			err = vpngate.ErrIdentity
		}
	}
	var candidate Session
	if err == nil {
		identity, e := vpngate.InspectIdentity(ctx, node)
		err = e
		if err == nil {
			key, e := security.Token(12)
			err = e
			if err == nil {
				var front *vpnipc.TransportProxy
				mode := "direct"
				if s.primary.FrontProxy != nil {
					front = &vpnipc.TransportProxy{Config: *s.primary.FrontProxy, Password: s.primary.FrontProxy.Password}
					mode = s.primary.FrontProxy.Kind
				}
				legacy := s.policy.AllowLegacy
				if target == s.primary.NodeID {
					legacy = s.primary.AllowLegacy
				}
				native, e := s.client.Start(s.ctx, key, vpnipc.Config{Front: front, Profile: node.Config, IP: node.IP, PeerName: identity.PeerName, PeerFingerprint: identity.PeerFingerprint, AllowLegacy: legacy})
				err = e
				if native != nil {
					candidate = &vpnSession{Session: native, transport: mode}
				}
			}
		}
	}
	if err == nil && candidate != nil {
		_, err = candidate.Probe(ctx)
	}
	// The VPN control lease must survive this startup timeout. Start uses the
	// wrapper lifetime; exchange and each identity/probe step have their own bounds.
	if candidate != nil && err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	if err == nil && !s.closed && s.ctx.Err() == nil && s.spare == nil {
		s.spare = candidate
		go s.watchSpare(candidate)
		s.status.Ready = true
		s.status.NodeID = target
		s.status.Reason = "备用隧道已实际连接并验证，故障时只切换新连接"
		candidate = nil
	} else {
		if err == nil {
			err = ErrClosed
		}
		s.status.Ready = s.spare != nil
		s.status.Reason = "备用未就绪；主通路保持，备用重试间隔两分钟"
	}
	s.status.CheckedAt = time.Now().UTC()
	s.mu.Unlock()
	if candidate != nil {
		_ = candidate.Close()
	}
}
func (s *standbySession) current() (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.closed
}
func (s *standbySession) Probe(ctx context.Context) (Proof, error) {
	active, closed := s.current()
	if closed {
		return Proof{}, ErrClosed
	}
	primary, cancel := context.WithTimeout(ctx, 3*time.Second)
	proof, err := active.Probe(primary)
	cancel()
	if err == nil && validProof(proof) {
		return proof, nil
	}
	if ctx.Err() != nil {
		return Proof{}, ctx.Err()
	}
	return s.promote(ctx, active)
}
func (s *standbySession) promote(ctx context.Context, observed Session) (Proof, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Proof{}, ErrClosed
	}
	if s.active != observed {
		active := s.active
		s.mu.Unlock()
		return active.Probe(ctx)
	}
	spare := s.spare
	s.mu.Unlock()
	if spare == nil {
		return Proof{}, ErrUnavailable
	}
	proof, err := spare.Probe(ctx)
	if err != nil || !validProof(proof) {
		return Proof{}, ErrProof
	}
	s.mu.Lock()
	old := s.active
	s.active = spare
	s.spare = nil
	s.status.ActiveNodeID = s.status.NodeID
	s.status.NodeID = ""
	s.status.Ready = false
	s.status.Promotions++
	s.status.CheckedAt = time.Now().UTC()
	s.status.Reason = "已提升真实验证的备用；旧连接需重连，正在等待备用补充"
	s.cleanup.Add(1)
	s.mu.Unlock()
	go func() { defer s.cleanup.Done(); _ = old.Close() }()
	go s.watch(spare)
	_ = s.source.AddEvent("warning", "exit.standby_promoted", "已提升同国家实际连接且验证的备用，入口地址/凭据保留；旧会话不能跨IP迁移", s.exit.ID)
	return proof, nil
}
func (s *standbySession) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	active, closed := s.current()
	if closed {
		return nil, ErrClosed
	}
	return active.DialContext(ctx, network, address)
}
func (s *standbySession) DialUDP(ctx context.Context, address string) (net.Conn, error) {
	active, closed := s.current()
	if closed {
		return nil, ErrClosed
	}
	udp, ok := active.(interface {
		DialUDP(context.Context, string) (net.Conn, error)
	})
	if !ok {
		return nil, ErrUnavailable
	}
	return udp.DialUDP(ctx, address)
}
func (s *standbySession) Close() error {
	s.closeOnce.Do(func() {
		s.operation.Lock()
		s.mu.Lock()
		s.closed = true
		active, spare := s.active, s.spare
		s.mu.Unlock()
		s.cancel()
		s.doneOnce.Do(func() { close(s.done) })
		s.operation.Unlock()
		if active != nil {
			s.closeErr = active.Close()
		}
		if spare != nil {
			s.closeErr = errors.Join(s.closeErr, spare.Close())
		}
		<-s.loopDone
		s.cleanup.Wait()
	})
	return s.closeErr
}

func (s *standbySession) watchSpare(session Session) {
	d, ok := session.(interface{ Done() <-chan struct{} })
	if !ok {
		return
	}
	select {
	case <-s.ctx.Done():
		return
	case <-d.Done():
	}
	s.mu.Lock()
	owns := s.spare == session
	if owns {
		s.spare = nil
		s.status.Ready = false
		s.status.NodeID = ""
		s.status.Reason = "备用会话已退出，等待有界重试"
		s.status.CheckedAt = time.Now().UTC()
	}
	s.mu.Unlock()
	if owns {
		_ = session.Close()
	}
}
