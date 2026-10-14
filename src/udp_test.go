package main

import (
	"net"
	"testing"
)

func TestParseUDPReqHeaderIPv4(t *testing.T) {
	b := []byte{0, 0, 0, atypIPv4, 8, 8, 8, 8, 0, 53, 1, 2, 3}
	atyp, addr, host, port, payload, ok := parseUDPReqHeader(b)
	if !ok || atyp != atypIPv4 || host != "8.8.8.8" || port != 53 || len(payload) != 3 || payload[0] != 1 {
		t.Fatalf("bad parse: ok=%v atyp=%v host=%v port=%v payload=%v", ok, atyp, host, port, payload)
	}
	if len(addr) != 4 || addr[0] != 8 {
		t.Fatalf("bad addr bytes: %v", addr)
	}
}

func TestParseUDPReqHeaderDomain(t *testing.T) {
	b := []byte{0, 0, 0, atypDomain, 3, 'd', 'n', 's', 0, 35, 9, 9}
	atyp, _, host, port, payload, ok := parseUDPReqHeader(b)
	if !ok || atyp != atypDomain || host != "dns" || port != 35 || len(payload) != 2 {
		t.Fatalf("bad parse: ok=%v atyp=%v host=%v port=%v payload=%v", ok, atyp, host, port, payload)
	}
}

func TestParseUDPReqHeaderBad(t *testing.T) {
	if _, _, _, _, _, ok := parseUDPReqHeader([]byte{1, 0, 0, atypIPv4, 1, 2, 3, 4, 0, 53}); ok {
		t.Fatal("rsv nonzero should fail")
	}
	ip6 := []byte{0, 0, 0, atypIPv6}
	ip6 = append(ip6, make([]byte, 16)...)
	ip6 = append(ip6, 0, 53)
	if _, _, _, _, _, ok := parseUDPReqHeader(ip6); ok {
		t.Fatal("ipv6 should fail")
	}
	if _, _, _, _, _, ok := parseUDPReqHeader([]byte{0, 0, 0, 0x09, 1, 2, 3, 4, 0, 53}); ok {
		t.Fatal("unknown atyp should fail")
	}
}

func TestBuildUDPRespHeader(t *testing.T) {
	pkt := buildUDPRespHeader(atypIPv4, []byte{8, 8, 8, 8}, 53, []byte{1, 2})
	if len(pkt) != 12 {
		t.Fatalf("bad packet len: %v", pkt)
	}
	head := pkt[:10]
	want := []byte{0, 0, 0, atypIPv4, 8, 8, 8, 8, 0, 53}
	for i := range want {
		if head[i] != want[i] {
			t.Fatalf("bad header: %v", pkt)
		}
	}
	if pkt[10] != 1 || pkt[11] != 2 {
		t.Fatalf("bad data: %v", pkt)
	}
}

func TestSocksReplyWith(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	go func() {
		_ = socksReplyWith(s, repSuccess, net.IPv4(1, 2, 3, 4), 5555)
		_ = s.Close()
	}()
	b := make([]byte, 10)
	if _, err := c.Read(b); err != nil {
		t.Fatal(err)
	}
	want := []byte{socksVer5, repSuccess, 0x00, atypIPv4, 1, 2, 3, 4, 0x15, 0xb3}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("bad reply: %v", b)
		}
	}
}
