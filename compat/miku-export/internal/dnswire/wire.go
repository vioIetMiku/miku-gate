// Package dnswire validates bounded DNS A/CNAME wire messages without resolving
// names or consulting hosts, NSS, search domains, or a system resolver.
package dnswire

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net/netip"
	"strings"
)

const MaxPacket = 4096

var ErrAnswer = errors.New("invalid bounded DNS response")

// Query returns a randomized wire question and its canonical absolute name.
func Query(host string) ([]byte, uint16, string, error) {
	name := strings.ToLower(strings.TrimSuffix(host, ".")) + "."
	if !ValidHostname(name) {
		return nil, 0, "", ErrAnswer
	}
	question, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, 0, "", ErrAnswer
	}
	var nonce [2]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, 0, "", err
	}
	id := binary.BigEndian.Uint16(nonce[:])
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: question, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	raw, err := msg.Pack()
	return raw, id, name, err
}

func ValidHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func Answer(raw []byte, id uint16, name string) ([]netip.Addr, string, bool, error) {
	if len(raw) < 12 || len(raw) > MaxPacket || binary.BigEndian.Uint16(raw[4:6]) != 1 {
		return nil, "", false, ErrAnswer
	}
	for _, offset := range []int{6, 8, 10} {
		if binary.BigEndian.Uint16(raw[offset:offset+2]) > 32 {
			return nil, "", false, ErrAnswer
		}
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(raw)
	if err != nil || header.ID != id || !header.Response || header.OpCode != 0 || header.RCode != dnsmessage.RCodeSuccess {
		return nil, "", false, ErrAnswer
	}
	question, err := parser.Question()
	if err != nil || strings.ToLower(question.Name.String()) != name || question.Type != dnsmessage.TypeA || question.Class != dnsmessage.ClassINET {
		return nil, "", false, ErrAnswer
	}
	if _, err = parser.Question(); !errors.Is(err, dnsmessage.ErrSectionDone) {
		return nil, "", false, ErrAnswer
	}
	if header.Truncated {
		return nil, "", true, nil
	}
	var message dnsmessage.Message
	if message.Unpack(raw) != nil {
		return nil, "", false, ErrAnswer
	}
	addresses := map[string][]netip.Addr{}
	aliases := map[string]string{}
	for _, answer := range message.Answers {
		if answer.Header.Class != dnsmessage.ClassINET {
			return nil, "", false, ErrAnswer
		}
		owner := strings.ToLower(answer.Header.Name.String())
		if !ValidHostname(owner) {
			return nil, "", false, ErrAnswer
		}
		switch resource := answer.Body.(type) {
		case *dnsmessage.AResource:
			addresses[owner] = append(addresses[owner], netip.AddrFrom4(resource.A))
			if len(addresses[owner]) > 8 {
				return nil, "", false, ErrAnswer
			}
		case *dnsmessage.CNAMEResource:
			next := strings.ToLower(resource.CNAME.String())
			if !ValidHostname(next) || aliases[owner] != "" {
				return nil, "", false, ErrAnswer
			}
			aliases[owner] = next
		default:
			return nil, "", false, ErrAnswer
		}
	}
	seen := map[string]bool{}
	current := name
	for i := 0; i < 8; i++ {
		if seen[current] {
			return nil, "", false, ErrAnswer
		}
		seen[current] = true
		ips, next := addresses[current], aliases[current]
		if len(ips) > 0 {
			if next != "" {
				return nil, "", false, ErrAnswer
			}
			return ips, "", false, nil
		}
		if next == "" {
			if current != name {
				return nil, current, false, nil
			}
			return nil, "", false, ErrAnswer
		}
		current = next
	}
	return nil, "", false, ErrAnswer
}
