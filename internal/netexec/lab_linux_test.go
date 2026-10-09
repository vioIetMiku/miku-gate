//go:build linux

package netexec

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These opt-in tests need a disposable Linux VM and real root capabilities.
// They never connect a veth to the host or contact a public VPN/Internet target.
func labEngine(t *testing.T) (*Engine, *linuxBackend) {
	t.Helper()
	if os.Getenv("MIKU_GATE_NETNS_TEST") != "1" {
		t.Skip("real kernel lab is opt-in: MIKU_GATE_NETNS_TEST=1")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the opted-in kernel lab requires root")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/run/miku-gate-netexec-tests", hex.EncodeToString(nonce[:]))
	e, err := openRuntime("lab_"+hex.EncodeToString(nonce[:]), true, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		e.Close()
		// Non-recursive cleanup only. Unexpected files or surviving records are kept.
		d := e.records.(*diskRecords)
		entries, err := os.ReadDir(d.directory)
		if err != nil {
			t.Errorf("inspect lab records: %v", err)
			return
		}
		if len(entries) != 1 || entries[0].Name() != ".lock" {
			t.Errorf("lab records retained for review: %s", d.directory)
			return
		}
		if err = os.Remove(filepath.Join(d.directory, ".lock")); err != nil {
			t.Error(err)
			return
		}
		if err = os.Remove(d.directory); err != nil {
			t.Error(err)
			return
		}
		if err = os.Remove(root); err != nil {
			t.Error(err)
		}
	})
	return e, e.backend.(*linuxBackend)
}
func labContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func labRun(t *testing.T, b *linuxBackend, ctx context.Context, ns *os.File, tool string, args ...string) []byte {
	t.Helper()
	raw, err := b.inNS(ctx, ns, tool, args, "")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func labHostSnapshot(t *testing.T, b *linuxBackend, ctx context.Context) []byte {
	t.Helper()
	var parts []any
	for _, command := range []struct {
		binary string
		args   []string
	}{
		{b.tools.ip, []string{"-j", "link", "show"}},
		{b.tools.ip, []string{"-j", "route", "show", "table", "all"}},
		{b.tools.ip, []string{"-j", "-6", "route", "show", "table", "all"}},
		{b.tools.nft, []string{"-j", "list", "ruleset"}},
	} {
		raw, err := runCommand(ctx, command.binary, command.args, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		stripVolatile(value)
		parts = append(parts, value)
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func stripVolatile(v any) {
	switch value := v.(type) {
	case []any:
		for _, item := range value {
			stripVolatile(item)
		}
	case map[string]any:
		delete(value, "expires")
		if counter, ok := value["counter"].(map[string]any); ok {
			delete(counter, "packets")
			delete(counter, "bytes")
		}
		for _, item := range value {
			stripVolatile(item)
		}
	}
}
func labCleanup(t *testing.T, e *Engine, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := e.Cleanup(ctx, id); err != nil {
		t.Errorf("lab cleanup of %s refused/failed: %v", id, err)
	}
}

func TestLinuxLabQuarantineAndHostIsolation(t *testing.T) {
	e, b := labEngine(t)
	ctx := labContext(t)
	before := labHostSnapshot(t, b, ctx)
	targetID, peerID := "lab_target_12345678", "lab_peer_12345678"
	target, err := e.Prepare(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { labCleanup(t, e, targetID) })
	if !target.QuarantineVerified || target.TunnelReady || target.ProxyListening {
		t.Fatal("misleading runtime state")
	}
	if !bytes.Equal(before, labHostSnapshot(t, b, ctx)) {
		t.Fatal("host network changed after preparation")
	}
	again, err := e.Prepare(ctx, targetID)
	if err != nil || again.Phase != "quarantined" {
		t.Fatal("prepare not idempotent", err)
	}
	peer, err := e.Prepare(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { labCleanup(t, e, peerID) })
	targetIdentity, _, err := b.Lookup(ctx, target.Plan)
	if err != nil {
		t.Fatal(err)
	}
	peerIdentity, _, err := b.Lookup(ctx, peer.Plan)
	if err != nil {
		t.Fatal(err)
	}
	targetNS, err := ownedNamespace(target.Plan, targetIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer targetNS.Close()
	peerNS, err := ownedNamespace(peer.Plan, peerIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer peerNS.Close()
	ping, err := trustedBinary("ping")
	if err != nil {
		t.Fatal(err)
	}

	// Create both ends from inside the peer namespace. No host interface, NAT,
	// forwarding setting, default route, DNS file or global nft table is touched.
	labRun(t, b, ctx, peerNS, b.tools.ip, "link", "add", "mp0", "type", "veth", "peer", "name", "mq0", "netns", target.Plan.Namespace)
	linkExists := true
	t.Cleanup(func() {
		if !linkExists {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ns, err := ownedNamespace(peer.Plan, peerIdentity)
		if err != nil {
			t.Errorf("lab veth retained: %v", err)
			return
		}
		defer ns.Close()
		if _, err = b.inNS(cleanup, ns, b.tools.ip, []string{"link", "delete", "dev", "mp0"}, ""); err != nil {
			t.Errorf("lab veth cleanup: %v", err)
		}
	})
	for _, end := range []struct {
		ns                 *os.File
		device, ipv4, ipv6 string
	}{
		{peerNS, "mp0", "198.18.0.1/30", "2001:db8:39::1/64"},
		{targetNS, "mq0", "198.18.0.2/30", "2001:db8:39::2/64"},
	} {
		labRun(t, b, ctx, end.ns, b.tools.ip, "address", "add", end.ipv4, "dev", end.device)
		labRun(t, b, ctx, end.ns, b.tools.ip, "-6", "address", "add", end.ipv6, "dev", end.device, "nodad")
		labRun(t, b, ctx, end.ns, b.tools.ip, "link", "set", "dev", end.device, "up")
	}
	if _, err = e.Cleanup(ctx, targetID); !errors.Is(err, ErrBusy) {
		t.Fatal("cleanup accepted extra interfaces", err)
	}
	// Positive control is essential: an unreachable test peer is not evidence of
	// filtering. Temporarily remove guards only in this disconnected lab topology.
	labRun(t, b, ctx, peerNS, b.tools.nft, "delete", "table", "inet", peer.Plan.Table)
	labRun(t, b, ctx, targetNS, b.tools.nft, "delete", "table", "inet", target.Plan.Table)
	probes := []struct {
		ns              *os.File
		family, address string
	}{
		{targetNS, "-4", "198.18.0.1"}, {peerNS, "-4", "198.18.0.2"},
		{targetNS, "-6", "2001:db8:39::1"}, {peerNS, "-6", "2001:db8:39::2"},
	}
	for _, probe := range probes {
		labRun(t, b, ctx, probe.ns, ping, probe.family, "-n", "-c", "1", "-W", "2", probe.address)
	}
	if err = b.InstallGuard(ctx, target.Plan, targetIdentity); err != nil {
		t.Fatal(err)
	}
	for _, probe := range probes {
		_, err = b.inNS(ctx, probe.ns, ping, []string{probe.family, "-n", "-c", "1", "-W", "1", probe.address}, "")
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("expected an actual packet-loss failure, got: %v", err)
		}
	}
	raw := labRun(t, b, ctx, targetNS, b.tools.nft, "-j", "list", "ruleset")
	if !guardMatches(raw, target.Plan) {
		t.Fatal("kernel policy differs from generated guard")
	}
	if !positiveDropCounter(raw, "input") || !positiveDropCounter(raw, "output") {
		t.Fatal("no kernel packet-drop counter evidence")
	}
	labRun(t, b, ctx, peerNS, b.tools.ip, "link", "delete", "dev", "mp0")
	linkExists = false
	status, err := e.Inspect(ctx, targetID)
	if err != nil || !status.QuarantineVerified {
		t.Fatal("guard not verified after lab topology removal", err)
	}
	if _, err = e.Cleanup(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Cleanup(ctx, peerID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, labHostSnapshot(t, b, ctx)) {
		t.Fatal("host links/routes/firewall changed during isolated lab")
	}
	t.Log("Observed: isolated IPv4/IPv6 positive controls, blocked ingress/egress, nft drop counters, extra-interface cleanup refusal, unchanged host network.")
}
func positiveDropCounter(raw []byte, chain string) bool {
	objects, ok := rulesetObjects(raw)
	if !ok {
		return false
	}
	for _, entry := range objects {
		rule, exists := entry["rule"]
		if !exists {
			continue
		}
		var r struct {
			Chain string `json:"chain"`
			Expr  []struct {
				Counter *struct {
					Packets uint64 `json:"packets"`
				} `json:"counter"`
			} `json:"expr"`
		}
		if json.Unmarshal(rule, &r) != nil || r.Chain != chain {
			continue
		}
		for _, expression := range r.Expr {
			if expression.Counter != nil && expression.Counter.Packets > 0 {
				return true
			}
		}
	}
	return false
}

func TestLinuxLabRefusesReplacedNamespace(t *testing.T) {
	e, b := labEngine(t)
	ctx := labContext(t)
	exitID := "lab_replace_12345678"
	status, err := e.Prepare(ctx, exitID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { labCleanup(t, e, exitID) })
	original, _, err := b.Lookup(ctx, status.Plan)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := ownedNamespace(status.Plan, original)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	if err = b.Delete(ctx, status.Plan, original); err != nil {
		t.Fatal(err)
	}
	replacement, err := b.Create(ctx, status.Plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := b.Delete(cleanup, status.Plan, replacement); err != nil {
			t.Errorf("remove explicitly created lab replacement: %v", err)
		}
	})
	if replacement == original {
		t.Fatal("namespace inode unexpectedly reused while old fd is held")
	}
	current, err := e.Inspect(ctx, exitID)
	if err != nil || current.Phase != "ownership-conflict" || current.QuarantineVerified {
		t.Fatal("replacement not detected", err)
	}
	if _, err = e.Cleanup(ctx, exitID); !errors.Is(err, ErrOwnership) {
		t.Fatal("deleted a replacement namespace", err)
	}
}

type failGuardBackend struct{ backend }

func (f failGuardBackend) InstallGuard(context.Context, Plan, Identity) error {
	return errors.New("lab: injected guard installation failure")
}
func TestLinuxLabRollsBackFailedPreparation(t *testing.T) {
	e, b := labEngine(t)
	ctx := labContext(t)
	e.backend = failGuardBackend{backend: b}
	status, err := e.Prepare(ctx, "lab_rollback_12345678")
	if err == nil || status.Phase != "absent" {
		t.Fatal("failed prepare was not rolled back", err)
	}
	if _, exists, err := b.Lookup(ctx, status.Plan); err != nil || exists {
		t.Fatal("rollback left a namespace", err)
	}
	if count, err := e.records.Count(); err != nil || count != 0 {
		t.Fatal("rollback left ownership records", err)
	}
}
