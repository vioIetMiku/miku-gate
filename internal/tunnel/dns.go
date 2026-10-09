package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"mikugate/internal/dnswire"
)

const maxDNSPacket = dnswire.MaxPacket

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
	ips, next, truncated, err := dnswire.Answer(raw, id, name)
	if err != nil {
		return nil, "", false, ErrDNS
	}
	return ips, next, truncated, nil
}
