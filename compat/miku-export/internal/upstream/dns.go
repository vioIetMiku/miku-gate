package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"time"

	"mikugate/internal/dnswire"
	"mikugate/internal/domain"
)

type resolver struct{ host, address string }

var resolvers = map[string]resolver{"cloudflare": {host: "cloudflare-dns.com", address: "1.1.1.1:443"}, "google": {host: "dns.google", address: "8.8.8.8:443"}}

func (c *Client) setupDNS() {
	selected := resolvers[c.config.DNS]
	c.transport = &http.Transport{
		Proxy: nil, DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 4 * time.Second, MaxResponseHeaderBytes: 16 * 1024,
		DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, ErrDNS },
		DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if (network != "tcp" && network != "tcp4") || address != net.JoinHostPort(selected.host, "443") {
				return nil, ErrDNS
			}
			conn, err := c.connectNumeric(ctx, selected.address)
			if err != nil {
				return nil, err
			}
			secured := tls.Client(conn, &tls.Config{ServerName: selected.host, RootCAs: c.roots, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
			stop := context.AfterFunc(ctx, func() { secured.Close() })
			err = secured.HandshakeContext(ctx)
			stop()
			if err != nil {
				secured.Close()
				return nil, ErrTLS
			}
			return secured, nil
		},
	}
	c.http = &http.Client{Transport: c.transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDNS }}
}
func (c *Client) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	selected := resolvers[c.config.DNS]
	seen := map[string]bool{}
	for attempt := 0; attempt < 5; attempt++ {
		if seen[host] || !domain.PublicHostname(host) {
			return nil, ErrDNS
		}
		seen[host] = true
		query, id, name, err := dnswire.Query(host)
		if err != nil {
			return nil, ErrDNS
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+selected.host+"/dns-query", bytes.NewReader(query))
		if err != nil {
			return nil, ErrDNS
		}
		request.Header.Set("Content-Type", "application/dns-message")
		request.Header.Set("Accept", "application/dns-message")
		response, err := c.http.Do(request)
		if err != nil {
			return nil, errors.Join(ErrDNS, knownError(err))
		}
		kind, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if response.StatusCode != http.StatusOK || mediaErr != nil || kind != "application/dns-message" || response.Header.Get("Content-Encoding") != "" {
			response.Body.Close()
			return nil, ErrDNS
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, dnswire.MaxPacket+1))
		response.Body.Close()
		if readErr != nil || len(raw) > dnswire.MaxPacket {
			return nil, ErrDNS
		}
		addresses, next, truncated, err := dnswire.Answer(raw, id, name)
		if err != nil || truncated {
			return nil, ErrDNS
		}
		if len(addresses) > 0 {
			return addresses, nil
		}
		if next == "" {
			return nil, ErrDNS
		}
		host = next
	}
	return nil, ErrDNS
}
func knownError(err error) error {
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrAuthentication, ErrTLS, ErrEndpoint, ErrProtocol, ErrClosed} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrDNS
}
