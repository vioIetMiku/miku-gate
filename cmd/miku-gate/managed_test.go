package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestManagedCLIConsentAndSecretInput(t *testing.T) {
	var out bytes.Buffer
	for _, command := range []string{"templates", "reality-candidates"} {
		if e := runIngress([]string{command}, strings.NewReader(""), &out); e != nil {
			t.Fatal(e)
		}
	}
	for _, args := range [][]string{{"certificate", "--domain=entry.example"}, {"reality-check", "--domain=entry.example"}, {"managed", "--bundle-dir=/etc/xray"}, {"managed", "--bundle-dir=/tmp/new", "--confirm-create"}} {
		out.Reset()
		if e := runIngress(args, strings.NewReader(`{"request":{},"proxy":{"password":"secret"}}`), &out); e == nil || strings.Contains(out.String(), "secret") {
			t.Fatal("missing consent or secret leaked")
		}
	}
}
