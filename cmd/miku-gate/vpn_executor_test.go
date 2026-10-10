package main

import (
	"bytes"
	"testing"
)

func TestVPNExecutorRequiresExplicitSeparateUIDs(t *testing.T) {
	for _, args := range [][]string{{}, {"exec"}, {"serve", "--instance=fixture_12345678"}, {"serve", "--instance=fixture_12345678", "--apply", "--allow-uid=1000", "--tunnel-uid=1000", "--worker-uid=1001"}, {"info", "--instance=fixture_12345678", "--apply"}, {"serve", "--instance=fixture_12345678", "--apply", "--allow-uid=1000", "--tunnel-uid=0", "--worker-uid=1001"}, {"serve", "--instance=fixture_12345678", "--program=/bin/sh"}} {
		var output bytes.Buffer
		if err := runVPNExecutor(args, &output); err == nil {
			t.Fatal("unsafe CLI arguments accepted", args)
		}
	}
}
