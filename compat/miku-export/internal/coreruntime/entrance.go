package coreruntime

import (
	"context"
	"net"
	"strconv"
	"time"

	"mikugate/internal/gateway"
	"mikugate/internal/ingress"
)

func EntranceKey(id string) string { return "entry:" + id }

// StartEntrance owns a single process for all landings. Individual VPN exits
// may stop without tearing down unrelated countries; unavailable local SOCKS
// endpoints fail closed inside the generated user routing.
func (m *Manager) StartEntrance(ctx context.Context, id string, base ingress.ManagedMaterial, routes []ingress.LandingRoute) (State, error) {
	key := EntranceKey(id)
	if len(routes) == 0 {
		return State{}, ErrExit
	}
	ready := 0
	for _, route := range routes {
		if m.exits.WithActive(route.ExitID, func(_ context.Context, state gateway.State) error {
			if state.Healthy && state.ProxyListening {
				ready++
			}
			return nil
		}) != nil {
			continue
		}
	}
	if ready == 0 {
		return State{}, ErrExit
	}
	raw, err := ingress.RenderLandings(base, routes)
	if err != nil {
		return State{}, err
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return State{}, ErrUnavailable
	}
	if m.entries[key] != nil || m.reserved[key] || len(m.entries)+len(m.checks) >= m.limit {
		m.mu.Unlock()
		return State{}, ErrBusy
	}
	life, cancel := context.WithCancel(m.ctx)
	e := &entry{cancel: cancel, done: make(chan struct{}), state: State{ExitID: key, Phase: "starting", Core: base.Request.Core, Listen: net.JoinHostPort(base.Request.Listen, strconv.Itoa(base.Request.Port)), Reason: "正在启动入口及按用户选择的多个OpenVPN落地"}}
	m.entries[key] = e
	m.mu.Unlock()
	stopRequest := context.AfterFunc(ctx, cancel)
	defer stopRequest()
	startup, stop := context.WithTimeout(life, 12*time.Second)
	stopStartup := context.AfterFunc(startup, cancel)
	defer func() { stopStartup(); stop() }()
	process, err := m.runner(life, base.Request.Core, raw, e.state.Listen)
	if err == nil && (startup.Err() != nil || !stopRequest() || ctx.Err() != nil) {
		err = ErrCore
	}
	if err != nil {
		cancel()
		if process != nil {
			process.Stop(context.Background())
		}
		m.mu.Lock()
		if m.entries[key] == e {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		close(e.done)
		return State{}, err
	}
	m.mu.Lock()
	e.process = process
	e.state.Phase = "running"
	e.state.PID = process.PID()
	e.state.ListenerOwned = true
	e.state.CoreValidated = true
	e.state.StartedAt = time.Now().UTC()
	e.state.Reason = "同一入口端口按独立客户端凭据选择各OpenVPN落地；停止的落地不会直连回退"
	m.mu.Unlock()
	go func() {
		select {
		case <-process.Done():
		case <-life.Done():
		}
		m.finish(key, e)
	}()
	state, _ := m.Snapshot(key)
	m.source.AddEvent("info", "entrance.started", "已启动多落地入口；各落地独立认证与路由", "")
	return state, nil
}
