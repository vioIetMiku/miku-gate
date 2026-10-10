package ingress

import (
	"encoding/hex"
	"net"
	"strconv"
	"strings"
)

type socket struct {
	address string
	port    int
}

func parseListeners(raw []byte, ipv6 bool) ([]socket, bool) {
	if len(raw) > 1<<20 {
		return nil, false
	}
	result := []socket{}
	for index, line := range strings.Split(string(raw), "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, false
		}
		if fields[3] != "0A" {
			continue
		}
		address, port, ok := strings.Cut(fields[1], ":")
		if !ok {
			return nil, false
		}
		n, e := strconv.ParseUint(port, 16, 16)
		if e != nil {
			return nil, false
		}
		b, e := hex.DecodeString(address)
		if e != nil || len(b) != 4 && !ipv6 || len(b) != 16 && ipv6 {
			return nil, false
		}
		for start := 0; start < len(b); start += 4 {
			b[start], b[start+3] = b[start+3], b[start]
			b[start+1], b[start+2] = b[start+2], b[start+1]
		}
		result = append(result, socket{net.IP(b).String(), int(n)})
		if len(result) > 4096 {
			return nil, false
		}
	}
	return result, true
}
func correlate(inbounds []Inbound, sockets []socket, complete bool) {
	for i := range inbounds {
		b := &inbounds[i]
		b.Listener = "unknown"
		if !complete {
			continue
		}
		b.Listener = "not_observed"
		for _, s := range sockets {
			if b.Port == s.port && b.Listen == s.address {
				b.Listener = "matching_address_port"
				break
			}
		}
	}
}
