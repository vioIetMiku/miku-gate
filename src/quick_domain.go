package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func managedQuickTemplate(ib *nativeInbound) bool {
	return ib.Enable && ib.BoundTo == "" && ib.AutoExitOrder == 0 &&
		strings.HasPrefix(ib.Remark, "Miku 一键 · ") && ib.Protocol == "vless" && ib.netOrTCP() == "tcp" &&
		len(ib.Clients) == 1 && ib.Clients[0].Enable && ib.Clients[0].BoundTo == "" && ib.Clients[0].Flow == "xtls-rprx-vision"
}

// Reuse the automatically managed family when changing a quick-setup domain.
// Keep listener ports, client credentials, exit orders and tunnel identities.
func retargetQuickFamily(store *nativeStore, templateID int, spec NewInboundSpec, tls *tlsConfig, reality *realityConfig) int {
	tpl := store.byID(templateID)
	if tpl == nil || !managedQuickTemplate(tpl) {
		return 0
	}
	var family []*nativeInbound
	for _, ib := range store.Inbounds {
		if ib == tpl || ib.AutoTemplate == tpl.ID && sameNativeTemplate(ib, tpl) {
			family = append(family, ib)
		}
	}
	for _, ib := range family {
		ib.Security, ib.TLS, ib.Reality = spec.Security, tls, reality
	}
	tpl.Remark = spec.Remark
	return len(family)
}

func (n *Native) reconfigureQuickTemplate(spec NewInboundSpec, tunnels []*Tunnel) (int, bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var template *nativeInbound
	for _, t := range tunnels {
		if ib := n.store.byID(t.TemplateID); ib != nil && managedQuickTemplate(ib) {
			template = ib
			break
		}
	}
	if template == nil {
		return 0, false, nil
	}
	var tls *tlsConfig
	var reality *realityConfig
	var err error
	if spec.Security == "tls" {
		tls, err = buildTLS(n.dir, spec)
	} else {
		reality, err = buildReality(n.proc.bin, spec)
	}
	if err != nil {
		return 0, false, err
	}
	before, err := json.Marshal(n.store)
	if err != nil {
		return 0, false, err
	}
	id := template.ID
	retargetQuickFamily(n.store, id, spec, tls, reality)
	if err := n.apply(tunnels); err != nil {
		var previous nativeStore
		if json.Unmarshal(before, &previous) == nil {
			n.store = &previous
			if restoreErr := n.apply(tunnels); restoreErr != nil {
				return 0, false, fmt.Errorf("更新节点域名失败，恢复核心也未完成：%v；%w", restoreErr, err)
			}
		}
		return 0, false, fmt.Errorf("更新节点域名失败，已恢复原配置：%w", err)
	}
	invalidateInbounds()
	return id, true, nil
}
