package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

func sameNativeTemplate(a, b *nativeInbound) bool {
	if a.Protocol != b.Protocol || a.netOrTCP() != b.netOrTCP() || a.securityOrNone() != b.securityOrNone() || a.Path != b.Path || a.Host != b.Host || !reflect.DeepEqual(a.TLS, b.TLS) || !reflect.DeepEqual(a.Reality, b.Reality) || len(a.Clients) != len(b.Clients) {
		return false
	}
	identities := func(ib *nativeInbound) []string {
		keys := make([]string, 0, len(ib.Clients))
		for _, c := range ib.Clients {
			key, _ := json.Marshal([]any{c.ID, c.Password, c.Flow, c.Enable, c.BoundTo})
			keys = append(keys, string(key))
		}
		sort.Strings(keys)
		return keys
	}
	return reflect.DeepEqual(identities(a), identities(b))
}

// Stable creation order identifies an automatically managed entry independently
// of hostnames, which can change on failover. Duplicate legacy clones are
// disabled only when they have identical credentials and automatic remarks.
func reconcileNativeEntry(store *nativeStore, tpl *nativeInbound, t *Tunnel, allocate func(map[int]bool) (int, error)) (bool, error) {
	host := sanitizeTag(t.Node.HostName)
	var selected *nativeInbound
	for _, ib := range store.Inbounds {
		if t.Order != 0 && ib.AutoExitOrder == t.Order {
			selected = ib
			break
		}
		if ib.Enable && ib.BoundTo == host && (sameNativeTemplate(ib, tpl) || tpl.ID == ib.ID) && selected == nil {
			selected = ib
		}
	}
	changed := false
	if selected == nil {
		port, err := allocate(store.usedPorts())
		if err != nil {
			return false, err
		}
		clone := *tpl
		clone.ID, clone.Port = store.NextID, port
		clone.Clients = append([]nativeClient(nil), tpl.Clients...)
		for i := range clone.Clients {
			clone.Clients[i].BoundTo = ""
		}
		taken := map[string]bool{}
		for _, ib := range store.Inbounds {
			taken[ib.Remark] = true
		}
		clone.Remark = uniqueRemark(exitLabel(t), taken)
		store.NextID++
		store.Inbounds = append(store.Inbounds, &clone)
		selected, changed = &clone, true
	}
	if selected.BoundTo != host || !selected.Enable || selected.AutoExitOrder != t.Order || selected.AutoTemplate != tpl.ID {
		selected.BoundTo, selected.Enable, selected.AutoExitOrder, selected.AutoTemplate = host, true, t.Order, tpl.ID
		changed = true
	}
	for _, ib := range store.Inbounds {
		if ib == selected || !ib.Enable || ib.BoundTo != host || !isGeneratedLabel(ib.Remark) || ib.AutoExitOrder != 0 && ib.AutoExitOrder != t.Order || !sameNativeTemplate(ib, selected) {
			continue
		}
		ib.Enable = false
		changed = true
	}
	return changed, nil
}

func (n *Native) ensureExitInbound(templateID int, t *Tunnel, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	tpl := n.store.byID(templateID)
	if tpl == nil {
		for _, ib := range n.store.Inbounds {
			if ib.Enable && (ib.AutoExitOrder == t.Order && t.Order != 0 || ib.BoundTo == sanitizeTag(t.Node.HostName)) {
				return nil
			}
		}
		return fmt.Errorf("入口模板 %d 已不存在，请重新选择入口", templateID)
	}
	before, err := json.Marshal(n.store)
	if err != nil {
		return err
	}
	changed, err := reconcileNativeEntry(n.store, tpl, t, freeRandomPort)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := n.apply(tunnels); err != nil {
		var old nativeStore
		if json.Unmarshal(before, &old) == nil {
			n.store = &old
		}
		return err
	}
	return nil
}
