package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func dnsName(t *testing.T, name string) dnsmessage.Name {
	t.Helper()
	n, err := dnsmessage.NewName(name)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func dnsA(t *testing.T, name, ip string) dnsmessage.Resource {
	return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsName(t, name), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: netip.MustParseAddr(ip).As4()}}
}
func dnsCNAME(t *testing.T, name, next string) dnsmessage.Resource {
	return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsName(t, name), Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: dnsName(t, next)}}
}
func dnsReply(t *testing.T, id uint16, name string, answers ...dnsmessage.Resource) []byte {
	t.Helper()
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: id, Response: true}, Questions: []dnsmessage.Question{{Name: dnsName(t, name), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}, Answers: answers}
	raw, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestDNSAnswerValidation(t *testing.T) {
	good := dnsReply(t, 39, "example.test.", dnsA(t, "example.test.", "8.8.8.8"))
	ips, _, _, err := dnsAnswer(good, 39, "example.test.")
	if err != nil || len(ips) != 1 || ips[0].String() != "8.8.8.8" {
		t.Fatal(ips, err)
	}
	cases := map[string][]byte{
		"id":          dnsReply(t, 40, "example.test.", dnsA(t, "example.test.", "8.8.8.8")),
		"question":    dnsReply(t, 39, "other.test.", dnsA(t, "example.test.", "8.8.8.8")),
		"unrelated-a": dnsReply(t, 39, "example.test.", dnsA(t, "other.test.", "8.8.8.8")),
		"loop":        dnsReply(t, 39, "example.test.", dnsCNAME(t, "example.test.", "next.test."), dnsCNAME(t, "next.test.", "example.test.")),
		"branching":   dnsReply(t, 39, "example.test.", dnsCNAME(t, "example.test.", "one.test."), dnsCNAME(t, "example.test.", "two.test.")),
		"ambiguous":   dnsReply(t, 39, "example.test.", dnsCNAME(t, "example.test.", "next.test."), dnsA(t, "example.test.", "8.8.8.8")),
		"missing":     {}, "large": make([]byte, maxDNSPacket+1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := dnsAnswer(raw, 39, "example.test."); !errors.Is(err, ErrDNS) {
				t.Fatal("invalid reply accepted", err)
			}
		})
	}
	alias := dnsReply(t, 39, "example.test.", dnsCNAME(t, "example.test.", "next.test."), dnsA(t, "next.test.", "1.1.1.1"))
	ips, _, _, err = dnsAnswer(alias, 39, "example.test.")
	if err != nil || len(ips) != 1 || ips[0].String() != "1.1.1.1" {
		t.Fatal(ips, err)
	}
	follow := dnsReply(t, 39, "example.test.", dnsCNAME(t, "example.test.", "next.test."))
	_, next, _, err := dnsAnswer(follow, 39, "example.test.")
	if err != nil || next != "next.test." {
		t.Fatal(next, err)
	}
}
func TestExplicitDNSRetriesTCPOnlyOnTruncation(t *testing.T) {
	calls := []string{}
	done := make(chan struct{}, 2)
	connect := func(_ context.Context, network, address string) (net.Conn, error) {
		if address != "10.243.0.1:15353" {
			t.Fatal("used an implicit resolver", address)
		}
		calls = append(calls, network)
		a, b := net.Pipe()
		go func() {
			defer func() { b.Close(); done <- struct{}{} }()
			raw := make([]byte, 1024)
			var size int
			if network == "tcp4" {
				var prefix [2]byte
				if _, err := io.ReadFull(b, prefix[:]); err != nil {
					return
				}
				size = int(binary.BigEndian.Uint16(prefix[:]))
				if size > len(raw) {
					return
				}
				if _, err := io.ReadFull(b, raw[:size]); err != nil {
					return
				}
			} else {
				var err error
				size, err = b.Read(raw)
				if err != nil {
					return
				}
			}
			var query dnsmessage.Message
			if query.Unpack(raw[:size]) != nil {
				return
			}
			if len(query.Questions) != 1 || query.Questions[0].Name.String() != "localhost." {
				t.Error("host suffix/hosts-file was used")
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, Truncated: network == "udp4"}, Questions: query.Questions}
			if network == "tcp4" {
				reply.Answers = []dnsmessage.Resource{dnsA(t, "localhost.", "8.8.8.8")}
			}
			data, err := reply.Pack()
			if err != nil {
				return
			}
			if network == "tcp4" {
				var prefix [2]byte
				binary.BigEndian.PutUint16(prefix[:], uint16(len(data)))
				data = append(prefix[:], data...)
			}
			_, _ = b.Write(data)
		}()
		return a, nil
	}
	result, err := lookupDNS(t.Context(), "localhost", netip.MustParseAddrPort("10.243.0.1:15353"), connect)
	if err != nil || len(result) != 1 || result[0].String() != "8.8.8.8" {
		t.Fatal("DNS consulted hosts or failed explicit resolver", result, err)
	}
	<-done
	<-done
	if len(calls) != 2 || calls[0] != "udp4" || calls[1] != "tcp4" {
		t.Fatal(calls)
	}
}
func FuzzDNSAnswer(f *testing.F) {
	f.Add([]byte{0, 39, 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0})
	f.Add([]byte("invalid"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		ips, next, _, err := dnsAnswer(raw, 39, "example.test.")
		if err == nil {
			if len(ips) > 8 {
				t.Fatal("unbounded answers")
			}
			if next != "" && !validHostname(next) {
				t.Fatal("invalid alias accepted")
			}
		}
	})
}
