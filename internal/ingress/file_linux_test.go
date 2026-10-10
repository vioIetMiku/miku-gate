//go:build linux

package ingress

import "testing"

func TestExplicitPanelDirectoryOwnerIsReadOnlyAndNarrow(t *testing.T) {
	source, _ := FindSource("x-ui")
	for index := 0; index < 5; index++ {
		if !directoryOwnerAllowed(source, index, 5, 0, 0) {
			t.Fatal("root owner unexpectedly rejected")
		}
		if directoryOwnerAllowed(source, index, 5, 1001, 0) {
			t.Fatal("nonroot implicitly accepted")
		}
		expected := index == 2 || index == 3
		if directoryOwnerAllowed(source, index, 5, 1001, 1001) != expected {
			t.Fatal("owner exception outside panel directories")
		}
		if directoryOwnerAllowed(source, index, 5, 1002, 1001) {
			t.Fatal("different owner accepted")
		}
	}
	other, _ := FindSource("xray-local")
	if _, e := ReadPanelSource(other, 1001); e == nil {
		t.Fatal("owner exception for arbitrary source")
	}
	if _, e := ReadPanelSource(source, 0); e == nil {
		t.Fatal("implicit root exception")
	}
}
