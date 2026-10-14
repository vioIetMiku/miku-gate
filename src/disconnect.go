package main

import "fmt"

// Disconnect preserves credentials but disables the users of the stopped exit.
// Other clients of a shared entrance retain their own working bindings.
func disableNativeExit(store *nativeStore, hosts map[string]bool) bool {
	changed := false
	for _, ib := range store.Inbounds {
		active := false
		affected := false
		for index := range ib.Clients {
			client := &ib.Clients[index]
			bound := client.BoundTo
			if bound == "" {
				bound = ib.BoundTo
			}
			if hosts[bound] && client.Enable {
				client.Enable = false
				affected = true
				changed = true
			}
			active = active || client.Enable
		}
		if ib.Enable && (hosts[ib.BoundTo] && len(ib.Clients) == 0 || affected && !active) {
			ib.Enable = false
			changed = true
		}
	}
	return changed
}

func (n *Native) DisconnectExit(hosts map[string]bool, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !disableNativeExit(n.store, hosts) {
		return nil
	}
	if err := n.store.save(n.dir); err != nil {
		return err
	}
	return n.apply(tunnels)
}

func (m *Manager) disconnectExitNodes(t *Tunnel) error {
	panel, err := openPanel()
	if err != nil {
		return err
	}
	hosts := map[string]bool{sanitizeTag(t.Node.HostName): true}
	if previous := t.prevHostOf(); previous != "" {
		hosts[sanitizeTag(previous)] = true
	}
	if native, ok := panel.(*Native); ok {
		return native.DisconnectExit(hosts, m.Tunnels())
	}
	if _, ok := panel.(*XCL); ok {
		return nil
	} // Its bound SOCKS outbound now has no listener.
	inbounds, err := panel.Inbounds(hosts)
	if err != nil {
		return err
	}
	disabled := false
	for _, inbound := range inbounds {
		if inbound.Enable && hosts[inbound.BoundTo] {
			if err := panel.UpdateInbound(inbound.ID, InboundPatch{Enable: &disabled}, m.Tunnels()); err != nil {
				return fmt.Errorf("入口%d: %w", inbound.ID, err)
			}
		}
	}
	return nil
}
