package netexec

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPlansAreDeterministicAndIsolated(t *testing.T) {
	first, err := BuildPlan("instance_12345678", "exit_123456789012")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := BuildPlan(first.InstanceID, first.ExitID)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("plan is not stable")
	}
	if first.TunnelEnabled || first.ProxyEnabled || first.HostRoutesChanged || first.HostFirewallChanged {
		t.Fatal("quarantine plan enabled a data path or host mutation")
	}
	if first.Stage != "quarantine-only" || strings.Count(first.Ruleset, "policy drop") != 3 || strings.Count(first.Ruleset, "counter drop") != 3 {
		t.Fatal("missing fail-closed policy")
	}
	for _, forbidden := range []string{"flush", "accept", "veth", "masquerade", "redirect"} {
		if strings.Contains(first.Ruleset, forbidden) {
			t.Fatalf("unsafe ruleset: %s", forbidden)
		}
	}
	names := map[string]bool{first.Namespace: true}
	for i := 0; i < 1000; i++ {
		p, err := BuildPlan(fmt.Sprintf("instance_%08d", i), fmt.Sprintf("exit_%08d", i))
		if err != nil || names[p.Namespace] || len(p.Namespace) > 32 {
			t.Fatalf("name collision or invalid name at %d", i)
		}
		names[p.Namespace] = true
	}
	other, _ := BuildPlan("instance_abcdefgh", first.ExitID)
	if other.Namespace == first.Namespace {
		t.Fatal("instances share resources")
	}
}
func TestPlanRejectsUnsafeInput(t *testing.T) {
	for _, value := range []string{"", "short", "../../../../etc", "name;reboot", "hello\nworld", "name with spaces", "name/123456789", "name\x0012345", "中文实例12345678", strings.Repeat("a", 65)} {
		if _, err := BuildPlan(value, "exit_12345678"); err == nil {
			t.Errorf("accepted instance %q", value)
		}
		if _, err := BuildPlan("instance_12345678", value); err == nil {
			t.Errorf("accepted exit %q", value)
		}
	}
}
