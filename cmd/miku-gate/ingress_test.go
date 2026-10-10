package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestIngressCLIRequiresFixedSourceAndConsent(t *testing.T) {
	var out bytes.Buffer
	if e := runIngress([]string{"sources"}, strings.NewReader(""), &out); e != nil || !strings.Contains(out.String(), "sing-box") {
		t.Fatal("sources command")
	}
	for _, args := range [][]string{{"inspect", "--source=x-ui"}, {"inspect", "--source=/etc/shadow", "--confirm-read"}, {"inspect", "--source=x-ui", "--confirm-read", "--output=/etc/shadow"}, {"prepare", "--source=/etc/shadow", "--confirm-read", "--confirm-create"}, {"prepare", "--source=x-ui", "--confirm-read", "--allow-install-owner=1001"}, {"inspect", "--source=x-ui", "--confirm-read", "--allow-install-owner=0"}} {
		out.Reset()
		if e := runIngress(args, strings.NewReader("secret"), &out); e == nil || strings.Contains(out.String(), "secret") {
			t.Fatal("unsafe CLI accepted")
		}
	}
}
