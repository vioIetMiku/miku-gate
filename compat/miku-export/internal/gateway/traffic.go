package gateway

import (
	"mikugate/internal/proxy"
	"time"
)

type Traffic struct {
	proxy.Snapshot
	Since   time.Time  `json:"since"`
	EndedAt *time.Time `json:"ended_at,omitempty"`
	Scope   string     `json:"scope"`
}

// Traffic returns a live snapshot or the last completed run in this process.
// It is deliberately not a durable, billable or host/interface traffic meter.
func (m *Manager) Traffic(id string) (Traffic, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil && !e.state.StartedAt.IsZero() {
		return trafficFor(e), true
	}
	v, ok := m.lastTraffic[id]
	return v, ok
}
func trafficFor(e *entry) Traffic {
	return Traffic{Snapshot: e.metrics.Snapshot(), Since: e.state.StartedAt, Scope: "proxy_tcp_forwarding"}
}
func (m *Manager) stateFor(e *entry) State {
	state := e.state
	if standby, ok := e.session.(interface{ StandbyStatus() StandbyStatus }); ok {
		v := standby.StandbyStatus()
		state.Standby = &v
	}
	if !state.StartedAt.IsZero() {
		v := trafficFor(e)
		state.Traffic = &v
	}
	return state
}
func (m *Manager) rememberTraffic(e *entry) {
	if e.state.StartedAt.IsZero() {
		return
	}
	v := trafficFor(e)
	end := time.Now().UTC()
	v.EndedAt = &end
	m.lastTraffic[e.state.ExitID] = v
	if len(m.lastTraffic) > 128 {
		oldest := ""
		for id, value := range m.lastTraffic {
			if oldest == "" || value.EndedAt.Before(*m.lastTraffic[oldest].EndedAt) {
				oldest = id
			}
		}
		delete(m.lastTraffic, oldest)
	}
}
func (m *Manager) ForgetTraffic(id string) { m.mu.Lock(); delete(m.lastTraffic, id); m.mu.Unlock() }
