package management

import "testing"

func TestCanonicalHTTPSOrigins(t *testing.T) {
	for _, v := range []string{"https://entry.example", "https://entry.example:8443", "https://192.0.2.10:8443", "https://[2001:db8::1]:8443"} {
		got, e := ParseOrigin(v)
		if e != nil || got.Origin != v {
			t.Fatalf("canonical %s", v)
		}
	}
	for _, v := range []string{"http://entry.example", "https://user@entry.example", "https://entry.example/", "https://entry.example?x=1", "https://entry.example#f", "https://ENTRY.EXAMPLE", "https://entry.example.", "https://0.0.0.0:8443", "https://[::]:8443", "https://entry.example:443", "https://entry.example:08443", "https://entry.example:0", "https://entry.example:65536", "https://entry.example\\evil"} {
		if _, e := ParseOrigin(v); e == nil {
			t.Fatalf("unsafe %s", v)
		}
	}
}
