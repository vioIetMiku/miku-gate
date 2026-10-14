package main

import (
	"fmt"
	"net/http"
	"strconv"
)

// Exit links are generated for this VPS and this exit's active clients only.
// Shared listeners can route different clients to different exits.
func (m *Manager) exitLinks(slot, inboundID int, host string) ([]string, error) {
	var exit *Tunnel
	for _, t := range m.Tunnels() {
		if t.Slot == slot {
			exit = t
			break
		}
	}
	if exit == nil || exit.Status != "up" {
		return nil, fmt.Errorf("出口尚未连通")
	}
	p, err := openPanel()
	if err != nil {
		return nil, err
	}
	bound := sanitizeTag(exit.Node.HostName)
	if n, ok := p.(*Native); ok {
		n.mu.Lock()
		defer n.mu.Unlock()
		var links []string
		for _, ib := range n.store.sorted() {
			if !ib.Enable || inboundID != 0 && ib.ID != inboundID {
				continue
			}
			for _, c := range ib.Clients {
				landing := c.BoundTo
				if landing == "" {
					landing = ib.BoundTo
				}
				if c.Enable && landing == bound {
					links = append(links, shareLink(ib, c, host))
				}
			}
		}
		return uniqueSubLinks(links), nil
	}
	list, err := p.Inbounds(nil)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, ib := range list {
		if ib.Enable && ib.BoundTo == bound && (inboundID == 0 || inboundID == ib.ID) {
			ids = append(ids, ib.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return p.InboundLinks(ids, host)
}

func apiExitLinks(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]string{"error": "只接受GET"})
			return
		}
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		id, idErr := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil || slot < 1 || r.URL.Query().Has("id") && (idErr != nil || id < 1) {
			writeJSON(w, 400, map[string]string{"error": "出口或入口参数无效"})
			return
		}
		links, err := m.exitLinks(slot, id, publicHost(r))
		if err != nil {
			writeJSON(w, 422, map[string]string{"error": err.Error()})
			return
		}
		if len(links) == 0 {
			writeJSON(w, 404, map[string]string{"error": "此出口没有可复制的启用节点，请先创建或修复入口"})
			return
		}
		writeJSON(w, 200, map[string]any{"links": links})
	}
}
