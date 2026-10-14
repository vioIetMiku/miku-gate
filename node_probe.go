package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type gateEndpoint struct {
	IP    string
	Port  int
	Proto string
}

// Only configured VPN endpoints are probed. UDP has no TCP handshake, so a
// UDP port must not be presented as a failed TCP service or guessed as 443.
func nodeProbeEndpoints(node Node) []gateEndpoint {
	proto, port := "udp", 1194
	var lines [][]string
	inline := false
	for _, raw := range strings.Split(node.Config, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "<") {
			inline = !strings.HasPrefix(line, "</")
			continue
		}
		if inline || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		for i := range parts {
			parts[i] = strings.Trim(parts[i], "\"'")
		}
		lines = append(lines, parts)
		if parts[0] == "proto" {
			proto = strings.ToLower(parts[1])
		}
		if parts[0] == "port" || parts[0] == "rport" {
			if n, e := strconv.Atoi(parts[1]); e == nil && n > 0 && n <= 65535 {
				port = n
			}
		}
	}
	var endpoints []gateEndpoint
	seen := map[string]bool{}
	for _, parts := range lines {
		if parts[0] != "remote" {
			continue
		}
		ip, err := publicLookupIP(parts[1])
		if err != nil {
			// A DNS remote may use the source's resolved public IP. No DNS lookup
			// or arbitrary local target is introduced by measurement.
			if net.ParseIP(parts[1]) != nil {
				continue
			}
			ip, err = publicLookupIP(node.IP)
			if err != nil {
				continue
			}
		}
		p, kind := port, proto
		if len(parts) > 2 {
			n, e := strconv.Atoi(parts[2])
			if e != nil || n < 1 || n > 65535 {
				continue
			}
			p = n
		}
		if len(parts) > 3 {
			kind = strings.ToLower(parts[3])
		}
		if !strings.HasPrefix(kind, "tcp") && !strings.HasPrefix(kind, "udp") {
			continue
		}
		key := ip + ":" + strconv.Itoa(p) + ":" + kind
		if !seen[key] {
			endpoints = append(endpoints, gateEndpoint{IP: ip, Port: p, Proto: kind})
			seen[key] = true
		}
		if len(endpoints) == 4 {
			break
		}
	}
	return endpoints
}

func latencyKey(node Node) string {
	key := gatePingTarget(node)
	for _, ep := range nodeProbeEndpoints(node) {
		key += "|" + ep.Proto + ":" + ep.IP + ":" + strconv.Itoa(ep.Port)
	}
	return key
}

func tcpEndpointLatency(ctx context.Context, ep gateEndpoint) (float64, error) {
	if _, err := publicLookupIP(ep.IP); err != nil || ep.Port < 1 || ep.Port > 65535 || !strings.HasPrefix(ep.Proto, "tcp") {
		return 0, fmt.Errorf("节点TCP端点无效")
	}
	var ms float64
	err := inMainNetns(func() error {
		network := "tcp4"
		if net.ParseIP(ep.IP).To4() == nil {
			network = "tcp6"
		}
		start := time.Now()
		conn, err := (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(ctx, network, net.JoinHostPort(ep.IP, strconv.Itoa(ep.Port)))
		if err != nil {
			return err
		}
		ms = float64(time.Since(start)) / float64(time.Millisecond)
		return conn.Close()
	})
	if err != nil {
		return 0, fmt.Errorf("VPN Gate配置中的TCP端口暂不可达")
	}
	return ms, nil
}
