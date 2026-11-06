package main

import (
	"io"
	"net/http"
	"sort"
	"strings"
)

// Terminal exports belong only to the managed 5/3/3 family. They are derived
// from persisted template/exit identities, so restarting does not create a new
// batch and a temporarily offline exit retains the same client credentials.
type terminalQuickView struct {
	Target   int      `json:"target"`
	Assigned int      `json:"assigned"`
	Ready    int      `json:"ready"`
	Complete bool     `json:"complete"`
	Existing bool     `json:"existing"`
	Links    []string `json:"links"`
	Job      string   `json:"job,omitempty"`
	Status   string   `json:"status,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

type terminalQuickExit struct {
	slot, template int
	host, country  string
	ready          bool
}

func (m *Manager) terminalQuickView(n *Native, host string) terminalQuickView {
	result := terminalQuickView{Target: 11, Links: []string{}}
	m.mu.RLock()
	exits := []terminalQuickExit{}
	for _, t := range m.tunnels {
		if t.quickCandidate.Load() || t.Status == "stopped" {
			continue
		}
		exits = append(exits, terminalQuickExit{t.Slot, t.TemplateID, sanitizeTag(t.Node.HostName), t.preferredRegion(), t.Status == "up" && t.EntryReady})
	}
	m.mu.RUnlock()
	sort.Slice(exits, func(i, j int) bool { return exits[i].slot < exits[j].slot })
	if n != nil {
		n.mu.Lock()
		defer n.mu.Unlock()
		for _, template := range n.store.sorted() {
			if !managedQuickTemplate(template) || template.securityOrNone() != "reality" || template.Reality == nil {
				continue
			}
			candidate := terminalQuickView{Target: 11, Links: []string{}}
			for _, region := range quickRegions {
				count := 0
				for _, exit := range exits {
					if exit.template != template.ID || exit.country != region.Code || count >= region.Count {
						continue
					}
					for _, inbound := range n.store.sorted() {
						if !inbound.Enable || inbound.BoundTo != exit.host || !sameNativeTemplate(inbound, template) {
							continue
						}
						if inbound.AutoTemplate != 0 && inbound.AutoTemplate != template.ID {
							continue
						}
						if len(inbound.Clients) != 1 || !inbound.Clients[0].Enable {
							continue
						}
						candidate.Links = append(candidate.Links, shareLink(inbound, inbound.Clients[0], host))
						candidate.Assigned++
						if exit.ready {
							candidate.Ready++
						}
						count++
						break
					}
				}
			}
			candidate.Complete = candidate.Assigned == 11
			if candidate.Assigned > result.Assigned {
				result = candidate
			}
		}
	}
	return result
}

func apiTerminalQuick(m *Manager, domains *DomainRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			writeJSON(w, 405, map[string]string{"error": "只接受 GET 或 POST"})
			return
		}
		panel, err := openPanel()
		var native *Native
		if err == nil {
			native, _ = panel.(*Native)
		}
		result := m.terminalQuickView(native, publishingHost())
		m.quickMu.Lock()
		if m.quickJob != nil {
			view := m.quickJob.View()
			result.Job, result.Status = view.ID, view.Status
			if view.Status == "failed" {
				for _, step := range view.Steps {
					if step.Status == "failed" {
						result.Detail = firstLine(step.Detail)
						break
					}
				}
			}
		}
		m.quickMu.Unlock()
		if r.Method == http.MethodPost && !result.Complete {
			// Fixed protocol and no certificate/domain picker in terminal mode.
			request := r.Clone(r.Context())
			request.Body = io.NopCloser(strings.NewReader(`{"domain":""}`))
			apiQuickSetup(m, domains).ServeHTTP(w, request)
			return
		}
		result.Existing = result.Complete
		writeJSON(w, 200, result)
	}
}
