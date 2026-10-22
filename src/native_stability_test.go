package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeResyncKeepsLiveCoreWhenRuntimeConfigIsUnchanged(t *testing.T) {
	for _, core := range []string{"xray", "sing-box"} {
		t.Run(core, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "fixture-core")
			// Accept either core's validation command, then stay alive as a
			// managed subprocess. No network ports or VPNs are opened.
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$2\" = \"-test\" ] || [ \"$1\" = \"check\" ]; then exit 0; fi\nexec /bin/sleep 60\n"), 0700); err != nil {
				t.Fatal(err)
			}
			n := &Native{dir: dir, proc: &xrayProc{core: core, bin: binary, dir: dir}, store: &nativeStore{NextID: 2, Inbounds: []*nativeInbound{{ID: 1, Port: 30000, Protocol: "vless", Enable: true, BoundTo: "original", Clients: []nativeClient{{ID: fixtureUUID, Enable: true}}}}}}
			cert := filepath.Join(dir, "certificate.pem")
			if err := os.WriteFile(cert, []byte("fixture certificate v1"), 0600); err != nil {
				t.Fatal(err)
			}
			n.store.Inbounds[0].Security = "tls"
			n.store.Inbounds[0].TLS = &tlsConfig{ServerName: "node.example.com", CertFile: cert, KeyFile: cert}
			t.Cleanup(n.Close)
			exit := &Tunnel{Slot: 1, Port: 35000, Status: "up", Node: Node{HostName: "original"}, Cred: SocksCred{User: "user", Pass: "pass"}}
			tunnels := []*Tunnel{exit}
			if err := n.OnTunnelsChanged(tunnels); err != nil {
				t.Fatal(err)
			}
			firstPID := n.proc.cmd.Process.Pid
			if err := n.ResyncOutbound(exit, tunnels); err != nil || n.proc.cmd.Process.Pid != firstPID {
				t.Fatal("same-node recovery restarted all client connections", err)
			}
			exit.Port++
			if err := n.ResyncOutbound(exit, tunnels); err != nil || n.proc.cmd.Process.Pid == firstPID {
				t.Fatal("real routing change failed to restart the core", err)
			}
			secondPID := n.proc.cmd.Process.Pid
			_ = n.proc.cmd.Process.Kill()
			<-n.proc.done
			if err := n.OnTunnelsChanged(tunnels); err != nil || n.proc.cmd.Process.Pid == secondPID {
				t.Fatal("configuration cache hid a dead core", err)
			}
			thirdPID := n.proc.cmd.Process.Pid
			if err := os.WriteFile(cert, []byte("fixture certificate v2"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := n.OnTunnelsChanged(tunnels); err != nil || n.proc.cmd.Process.Pid == thirdPID {
				t.Fatal("certificate renewal at the same path was not reloaded", err)
			}
		})
	}
}
