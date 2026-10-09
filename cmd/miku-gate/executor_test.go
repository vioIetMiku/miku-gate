package main

import (
	"bytes"
	"testing"
)

func TestExecutorRequiresExplicitAuthority(t *testing.T) {
	for _, args := range [][]string{
		{}, {"shell"}, {"serve"}, {"serve", "--instance=instance_12345678", "--allow-uid=1000"},
		{"serve", "--instance=instance_12345678", "--allow-uid=0", "--apply"},
		{"serve", "--instance=instance_12345678", "--allow-uid=4294967295", "--apply"},
		{"serve", "--instance=../../etc", "--allow-uid=1000", "--apply"},
		{"serve", "--instance=instance_12345678", "--allow-uid=1000", "--apply", "--socket=/tmp/socket"},
		{"info", "--instance=instance_12345678", "--apply"},
	} {
		if err := runExecutor(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("unsafe executor args accepted: %v", args)
		}
	}
}
