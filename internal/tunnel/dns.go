package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const maxDNSPacket = 4096

// Explicit wire DNS avoids net.Resolver's implicit /etc/hosts, NSS and search
// domain behavior. Every UDP/TCP exchange goes to the one configured resolver
// through the same interface-bound namespace connector used for business TCP.
func lookupDNS(ctx context.Context, host string, resolver netip.AddrPort, connect func(context.Context, string, string) (net.Conn, error)) ([]netip.Addr, error) {
	name := strings.ToLower(strings.TrimSuffix(host, ".")) + "."
	seen := map[string]bool{}
	for attempt := 0; attempt < 5; attempt++ {
		if seen[name] || !validHostname(name) {
			return nil, ErrDNS
		}
		seen[name] = true
		question, err := dnsmessage.NewName(name)
		if err != nil {
			return nil, ErrDNS
		}
		var nonce [2]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		id := binary.BigEndian.Uint16(nonce[:])
		message := dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: question, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
		query, err := message.Pack()
		if err != nil {
			return nil, ErrDNS
		}
		raw, err := dnsExchange(ctx, "udp4", resolver.String(), query, connect)
		if err != nil {
			return nil, err
		}
		addresses, next, truncated, err := dnsAnswer(raw, id, name)
		if err != nil {
			return nil, err
		}
		if truncated {
			raw, err = dnsExchange(ctx, "tcp4", resolver.String(), query, connect)
			if err != nil {
				return nil, err
			}
			addresses, next, truncated, err = dnsAnswer(raw, id, name)
			if err != nil || truncated {
				return nil, ErrDNS
			}
		}
		if len(addresses) > 0 {
			return addresses, nil
		}
		if next == "" {
			return nil, ErrDNS
		}
		name = next
	}
	return nil, ErrDNS
}
func dnsExchange(ctx context.Context, network, address string, query []byte, connect func(context.Context, string, string) (net.Conn, error)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := connect(ctx, network, address)
	if err != nil {
		return nil, ErrDNS
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, ErrDNS
	}
	if network == "tcp4" {
		var header [2]byte
		binary.BigEndian.PutUint16(header[:], uint16(len(query)))
		if _, err = io.Copy(conn, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(query))); err != nil {
			return nil, ErrDNS
		}
		if _, err = io.ReadFull(conn, header[:]); err != nil {
			return nil, ErrDNS
		}
		size := int(binary.BigEndian.Uint16(header[:]))
		if size < 12 || size > maxDNSPacket {
			return nil, ErrDNS
		}
		raw := make([]byte, size)
		if _, err = io.ReadFull(conn, raw); err != nil {
			return nil, ErrDNS
		}
		return raw, nil
	}
	if n, err := conn.Write(query); err != nil || n != len(query) {
		return nil, ErrDNS
	}
	raw := make([]byte, maxDNSPacket+1)
	n, err := conn.Read(raw)
	if err != nil || n > maxDNSPacket {
		return nil, ErrDNS
	}
	return raw[:n], nil
}
func dnsAnswer(raw []byte, id uint16, name string) ([]netip.Addr, string, bool, error) {
	if len(raw) < 12 || len(raw) > maxDNSPacket || binary.BigEndian.Uint16(raw[4:6]) != 1 {
		return nil, "", false, ErrDNS
	}
	for _, offset := range []int{6, 8, 10} {
		if binary.BigEndian.Uint16(raw[offset:offset+2]) > 32 {
			return nil, "", false, ErrDNS
		}
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(raw)
	if err != nil || header.ID != id || !header.Response || header.OpCode != 0 || header.RCode != dnsmessage.RCodeSuccess {
		return nil, "", false, ErrDNS
	}
	question, err := parser.Question()
	if err != nil || strings.ToLower(question.Name.String()) != name || question.Type != dnsmessage.TypeA || question.Class != dnsmessage.ClassINET {
		return nil, "", false, ErrDNS
	}
	if _, err = parser.Question(); !errors.Is(err, dnsmessage.ErrSectionDone) {
		return nil, "", false, ErrDNS
	}
	if header.Truncated {
		return nil, "", true, nil
	}
	var message dnsmessage.Message
	if message.Unpack(raw) != nil {
		return nil, "", false, ErrDNS
	}
	addresses := map[string][]netip.Addr{}
	aliases := map[string]string{}
	for _, answer := range message.Answers {
		if answer.Header.Class != dnsmessage.ClassINET {
			return nil, "", false, ErrDNS
		}
		owner := strings.ToLower(answer.Header.Name.String())
		if !validHostname(owner) {
			return nil, "", false, ErrDNS
		}
		switch resource := answer.Body.(type) {
		case *dnsmessage.AResource:
			addresses[owner] = append(addresses[owner], netip.AddrFrom4(resource.A))
			if len(addresses[owner]) > 8 {
				return nil, "", false, ErrDNS
			}
		case *dnsmessage.CNAMEResource:
			next := strings.ToLower(resource.CNAME.String())
			if !validHostname(next) || aliases[owner] != "" {
				return nil, "", false, ErrDNS
			}
			aliases[owner] = next
		default:
			return nil, "", false, ErrDNS
		}
	}
	seen := map[string]bool{}
	current := name
	for i := 0; i < 8; i++ {
		if seen[current] {
			return nil, "", false, ErrDNS
		}
		seen[current] = true
		ips, next := addresses[current], aliases[current]
		if len(ips) > 0 {
			if next != "" {
				return nil, "", false, ErrDNS
			}
			return ips, "", false, nil
		}
		if next == "" {
			if current != name {
				return nil, current, false, nil
			}
			return nil, "", false, ErrDNS
		}
		current = next
	}
	return nil, "", false, ErrDNS
}
