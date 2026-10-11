//go:build linux

package certassets

import (
	"strings"
	"testing"
)

func TestCandidatesStayInsideDomainSpecificAllowedRoots(t *testing.T) {
	host, e := Hostname("entry.example")
	if e != nil {
		t.Fatal(e)
	}
	list := candidates(host)
	if len(list) != 3 {
		t.Fatal("fixed roots")
	}
	for _, c := range list {
		if !strings.Contains(c.cert, host) || !strings.Contains(c.key, host) || !strings.Contains(c.root, host) {
			t.Fatal("unbound path")
		}
	}
	if _, _, e := Discover("../../etc/shadow"); e == nil {
		t.Fatal("path accepted")
	}
	if _, e := rootFile("/etc/../shadow", true); e == nil {
		t.Fatal("parent traversal allowed")
	}
}
