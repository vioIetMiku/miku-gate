//go:build linux

package coreruntime

import (
	"strings"
	"testing"
)

func TestCoreDiagnosticsNeverRetainRawValues(t *testing.T) {
	logs := new(safeCoreLogs)
	sensitive := []string{"private-key-secret", "uuid-secret", "proxy-password-secret"}
	for _, value := range sensitive {
		logs.Write([]byte("failed REALITY authentication: " + value + "\n"))
	}
	result := strings.Join(logs.Summary(), ",")
	for _, value := range sensitive {
		if strings.Contains(result, value) {
			t.Fatal("raw diagnostic leak")
		}
	}
	if !strings.Contains(result, "failed=3") {
		t.Fatal("missing fixed categories")
	}
	first := logs.Summary()
	first[0] = "caller mutation"
	if strings.Contains(strings.Join(logs.Summary(), ","), "caller mutation") {
		t.Fatal("mutable diagnostic backing")
	}
}
