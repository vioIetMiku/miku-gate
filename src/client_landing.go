package main

import (
	"fmt"
	"net/http"
	"strconv"
)

type clientLandingPanel interface {
	BindClient(int, string, string, []*Tunnel) error
}

func (n *Native) BindClient(id int, email, host string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("入口不存在")
	}
	if host != "" {
		ready := false
		for _, t := range tunnels {
			if t.Node.HostName == host && t.Status == "up" {
				ready = true
			}
		}
		if !ready {
			return fmt.Errorf("先等所选落地连通")
		}
	}
	for index := range ib.Clients {
		if ib.Clients[index].Email == email {
			old := ib.Clients[index].BoundTo
			ib.Clients[index].BoundTo = sanitizeTag(host)
			if err := n.apply(tunnels); err != nil {
				ib.Clients[index].BoundTo = old
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("客户端不存在")
}

func apiClientLanding(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "请用POST"})
			return
		}
		p, err := openPanel()
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		panel, ok := p.(clientLandingPanel)
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "此后端按节点端口选择落地；同端口多落地请使用自建核心"})
			return
		}
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "入口ID无效"})
			return
		}
		err = panel.BindClient(id, r.URL.Query().Get("email"), r.URL.Query().Get("host"), m.Tunnels())
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		invalidateInbounds()
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}
