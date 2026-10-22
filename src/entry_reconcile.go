package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func sameNodeEndpoint(a, b Node) bool {
	return a.HostName == b.HostName || a.IP != "" && a.IP == b.IP
}

// Claim and assignment are atomic across all parallel connection attempts.
func (m *Manager) claimCandidate(t *Tunnel, node Node) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || t.quickRetiring || t.Status == "stopped" {
		return false
	}
	if region := t.preferredRegion(); region != "" && normalizeRegion(node.CountryCode) != region {
		return false
	}
	for _, other := range m.tunnels {
		if other != t && sameNodeEndpoint(node, other.Node) {
			return false
		}
	}
	if t.Node.HostName != node.HostName && t.prevHostOf() == "" {
		t.setPrevHost(t.Node.HostName)
	}
	t.Node = node
	return true
}

func (m *Manager) acceptExitIP(t *Tunnel) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || t.quickRetiring || t.Status == "stopped" {
		return false
	}
	for _, other := range m.tunnels {
		if other != t && !other.quickRetiring && other.Status == "up" && other.ExitIP != "" && other.ExitIP == t.ExitIP {
			return false
		}
	}
	t.Status = "up"
	t.readyAt = time.Now()
	return true
}

// Each exit carries its desired template through retries and restarts. An
// idempotent reconciliation also repairs legacy exits that had no entry.
func (m *Manager) ensureTunnelEntry(t *Tunnel) error {
	m.entryMu.Lock()
	defer m.entryMu.Unlock()
	if !m.tunnelActive(t) || t.Status != "up" {
		return nil
	}
	if old := t.prevHostOf(); old != "" {
		if err := m.rebind(old, t); err != nil {
			return err
		}
		t.setPrevHost("")
	}
	if t.TemplateID == 0 {
		t.EntryReady = true
		t.EntryError = ""
		return nil
	}
	p, err := openPanel()
	if err != nil {
		return err
	}
	if t.TemplateID < 0 {
		list, err := p.Inbounds(nil)
		if err != nil {
			return err
		}
		for _, ib := range list {
			if ib.Enable && (ib.BoundTo == "" || ib.BoundTo == sanitizeTag(t.Node.HostName)) {
				t.TemplateID = ib.ID
				break
			}
		}
		if t.TemplateID < 0 {
			ib, err := p.CreateInbound(NewInboundSpec{Protocol: "vless", Network: "tcp", Security: "reality", Vision: true, Remark: "入口模板"}, m.Tunnels())
			if err != nil {
				return err
			}
			t.TemplateID = ib.ID
		}
	}
	tunnels := m.Tunnels()
	if t.quickCandidate.Load() {
		tunnels = append(tunnels, t)
	}
	if n, ok := p.(*Native); ok {
		err = n.ensureExitInbound(t.TemplateID, t, tunnels)
	} else {
		// Existing panel entries are adopted rather than cloned on every retry.
		list, listErr := p.Inbounds(nil)
		if listErr != nil {
			return listErr
		}
		found := false
		for _, ib := range list {
			if ib.Enable && ib.BoundTo == sanitizeTag(t.Node.HostName) {
				found = true
				break
			}
		}
		if !found {
			_, err = p.CloneToTunnels(t.TemplateID, []string{t.Node.HostName}, tunnels)
		}
	}
	if err != nil {
		t.EntryReady = false
		return fmt.Errorf("入口待修复: %w", err)
	}
	t.EntryReady, t.EntryError = true, ""
	invalidateInbounds()
	return m.saveState()
}

func apiEntryRepair(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "只接受POST"})
			return
		}
		slot, _ := strconv.Atoi(r.URL.Query().Get("slot"))
		m.mu.RLock()
		t := m.tunnels[slot]
		m.mu.RUnlock()
		if t == nil {
			writeJSON(w, 404, map[string]string{"error": "出口不存在"})
			return
		}
		if t.TemplateID == 0 {
			t.TemplateID = -1
		}
		t.EntryReady = false
		if err := m.ensureTunnelEntry(t); err != nil {
			t.EntryError = err.Error()
			writeJSON(w, 422, map[string]string{"error": err.Error()})
			return
		}
		_ = m.saveState()
		writeJSON(w, 200, map[string]any{"ready": t.EntryReady})
	}
}
