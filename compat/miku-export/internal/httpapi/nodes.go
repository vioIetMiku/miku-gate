package httpapi

import (
	"mikugate/internal/domain"
	"mikugate/internal/tunnel"
	"mikugate/internal/vpngate"
	"net/http"
)

func (a *Server) nodeProfile(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	node, err := a.store.NodeForRuntime(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	if node.Source != vpngate.Source {
		fail(w, 409, "untrusted_node_source", "此检查只接受官方来源缓存；没有执行或连接配置。")
		return
	}
	write(w, 200, tunnel.CheckProfile(node.Config, node.IP))
}
