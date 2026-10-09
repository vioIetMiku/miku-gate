package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"mikugate/internal/netexec"
)

func TestSandboxPlanAndExplicitApply(t *testing.T) {
	good := []string{"--instance=instance_12345678", "--exit=exit_123456789012"}
	var out bytes.Buffer
	if err := runSandbox(append([]string{"plan"}, good...), &out); err != nil {
		t.Fatal(err)
	}
	var p netexec.Plan
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Namespace == "" || p.ProxyEnabled || p.TunnelEnabled {
		t.Fatal("bad plan", err)
	}
	for _, operation := range []string{"prepare", "cleanup"} {
		out.Reset()
		if err := runSandbox(append([]string{operation}, good...), &out); err == nil {
			t.Fatal("mutation did not require --apply")
		}
		if out.Len() != 0 {
			t.Fatal("mutation emitted success without --apply")
		}
	}
	for _, args := range [][]string{
		{}, {"shell"}, {"plan"}, {"plan", "--instance=../../../../etc", "--exit=exit_123456789012"},
		{"plan", "--instance=instance_12345678", "--exit=exit_123456789012", "--apply"},
		{"plan", "--instance=instance_12345678", "--exit=exit_123456789012", "extra"},
		{"plan", "--instance=instance_12345678", "--exit=exit_123456789012", "--command=whoami"},
	} {
		if err := runSandbox(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}
