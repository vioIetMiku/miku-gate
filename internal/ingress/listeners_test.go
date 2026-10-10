package ingress

import (
	"strings"
	"testing"
)

func TestSocketMetadataDoesNotInventOwnership(t *testing.T) {
	raw := []byte("header\n 0: 0100007F:1F90 00000000:0000 0A 0 0 0\n 1: 00000000:01BB 00000000:0000 0A 0 0 0\n 2: 0100007F:FF00 00000000:0000 01 0 0 0\n")
	sockets, ok := parseListeners(raw, false)
	if !ok || len(sockets) != 2 || sockets[0].address != "127.0.0.1" || sockets[0].port != 8080 {
		t.Fatal("socket parse")
	}
	rows := []Inbound{{Listen: "127.0.0.1", Port: 8080}, {Listen: "0.0.0.0", Port: 443}, {Listen: "::", Port: 443}}
	correlate(rows, sockets, true)
	if rows[0].Listener != "matching_address_port" || rows[1].Listener != "matching_address_port" || rows[2].Listener != "not_observed" {
		t.Fatal("mismatch reported as matched")
	}
	correlate(rows, sockets, false)
	for _, row := range rows {
		if row.Listener != "unknown" {
			t.Fatal("failed observation treated as absence")
		}
	}
	for _, bad := range []string{"header\n 0: BROKEN:ABCD x 0A", strings.Repeat("x", (1<<20)+1)} {
		if _, ok := parseListeners([]byte(bad), false); ok {
			t.Fatal("bad observation accepted")
		}
	}
	sockets, ok = parseListeners([]byte("header\n 0: 00000000000000000000000001000000:01BB x 0A\n"), true)
	if !ok || sockets[0].address != "::1" {
		t.Fatal("IPv6 word endianness")
	}
}
