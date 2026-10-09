package netexec

import (
	"bytes"
	"encoding/json"
	"testing"
)

func guardFixture(p Plan) []byte {
	items := []any{map[string]any{"metainfo": map[string]any{"json_schema_version": 1}}, map[string]any{"table": map[string]any{"family": "inet", "name": p.Table, "handle": 7, "comment": p.Owner}}}
	for _, name := range []string{"input", "forward", "output"} {
		items = append(items, map[string]any{"chain": map[string]any{"family": "inet", "table": p.Table, "name": name, "type": "filter", "hook": name, "prio": -300, "policy": "drop"}})
		items = append(items, map[string]any{"rule": map[string]any{"family": "inet", "table": p.Table, "chain": name, "expr": []any{map[string]any{"counter": map[string]any{"packets": 42, "bytes": 420}}, map[string]any{"drop": nil}}}})
	}
	raw, _ := json.Marshal(map[string]any{"nftables": items})
	return raw
}
func TestGuardSnapshotMatchesOnlyExactKernelPolicy(t *testing.T) {
	p, _ := BuildPlan("instance_12345678", testExit)
	good := guardFixture(p)
	if !guardMatches(good, p) || !cleanupRulesetSafe(good, p) {
		t.Fatal("valid observed quarantine rejected")
	}
	replace := func(old, new string) []byte { return bytes.Replace(good, []byte(old), []byte(new), 1) }
	bad := map[string][]byte{
		"missing": nil, "empty": []byte("{}"), "null": []byte(`{"nftables":null}`),
		"duplicate-policy": replace(`"policy":"drop"`, `"policy":"accept","policy":"drop"`),
		"default-accept":   replace(`"policy":"drop"`, `"policy":"accept"`),
		"accept-rule":      replace(`"drop":null`, `"accept":null`),
		"extra-expression": replace(`"drop":null`, `"drop":null,"accept":null`),
		"bad-verdict":      replace(`"drop":null`, `"drop":{}`),
		"IPv4-only":        replace(`"family":"inet"`, `"family":"ip"`),
		"dormant-table":    replace(`"handle":7`, `"handle":7,"flags":["dormant"]`),
		"wrong-owner":      replace(p.Owner, "some-other-owner"),
		"wrong-priority":   replace(`"prio":-300`, `"prio":0`),
		"wrong-hook":       replace(`"hook":"input"`, `"hook":"prerouting"`),
		"negative-counter": replace(`"bytes":420`, `"bytes":-1`),
		"duplicate-chain":  replace(`"hook":"forward"`, `"hook":"input"`),
		"trailing-data":    append(append([]byte{}, good...), []byte("{}")...),
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if guardMatches(raw, p) {
				t.Fatal("unsafe or ambiguous policy reported protected")
			}
		})
	}
	if emptyRuleset(good) || !emptyRuleset([]byte(`{"nftables":[{"metainfo":{}}]}`)) {
		t.Fatal("wrong empty-ruleset detection")
	}
	for _, raw := range [][]byte{bad["wrong-owner"], bad["IPv4-only"], bad["missing"]} {
		if cleanupRulesetSafe(raw, p) {
			t.Fatal("foreign/invalid ruleset allowed for cleanup")
		}
	}
}
func TestOnlyLoopbackIsSafeToDelete(t *testing.T) {
	if !onlyLoopback([]byte(`[{"ifname":"lo","link_type":"loopback","flags":["LOOPBACK"]}]`)) {
		t.Fatal("valid loopback rejected")
	}
	for _, raw := range []string{`[]`, `null`, `{}`, `[{"ifname":"lo","link_type":"ether"}]`, `[{"ifname":"lo","link_type":"loopback"},{"ifname":"eth0","link_type":"ether"}]`} {
		if onlyLoopback([]byte(raw)) {
			t.Fatal("extra/non-loopback interface accepted")
		}
	}
}
func FuzzGuardSnapshot(f *testing.F) {
	p, _ := BuildPlan("instance_12345678", testExit)
	f.Add(guardFixture(p))
	f.Add([]byte(`{"nftables":[]}`))
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if guardMatches(raw, p) && !cleanupRulesetSafe(raw, p) {
			t.Fatal("verified policy is not owned")
		}
	})
}
func TestClosedOrReadOnlyEngineCannotMutate(t *testing.T) {
	e, records, b := fixture(t)
	e.readOnly = true
	if _, err := e.Prepare(t.Context(), testExit); err == nil {
		t.Fatal("read-only engine prepared namespace")
	}
	if _, err := e.Cleanup(t.Context(), testExit); err == nil {
		t.Fatal("read-only engine cleaned namespace")
	}
	if len(records.values) != 0 || len(b.calls) != 0 {
		t.Fatal("read-only session mutated state")
	}
	e.readOnly = false
	count := 0
	e.close = func() error { count++; return nil }
	e.Close()
	e.Close()
	if count != 1 {
		t.Fatal("lock released more than once")
	}
	if _, err := e.Inspect(t.Context(), testExit); err == nil {
		t.Fatal("closed engine inspected resources")
	}
	if _, err := e.Prepare(t.Context(), testExit); err == nil {
		t.Fatal("closed engine mutated resources")
	}
}
