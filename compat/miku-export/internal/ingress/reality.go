package ingress

import (
	"context"
	"crypto/tls"
	"errors"
	"mikugate/internal/certassets"
	"mikugate/internal/domain"
	"mikugate/internal/egress"
	"net"
	"time"
)

type RealityCandidate struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Hint     string `json:"hint"`
}

func RealityCandidates() []RealityCandidate {
	names := []string{"www.microsoft.com", "www.cloudflare.com", "www.bing.com", "www.amazon.com", "www.yahoo.com", "www.samsung.com", "www.mozilla.org", "www.wikipedia.org", "www.nvidia.com", "www.github.com"}
	out := make([]RealityCandidate, 0, len(names))
	for _, host := range names {
		out = append(out, RealityCandidate{host, 443, "候选不是可用性保证；先从VPS验证TLS1.3/X25519/h2及证书，再选择。不是管理域名，不代表运营者所在地。"})
	}
	return out
}

type RealityTarget struct {
	Hostname    string    `json:"hostname"`
	Address     string    `json:"address"`
	Port        int       `json:"port"`
	CheckedAt   time.Time `json:"checked_at"`
	NotAfter    time.Time `json:"not_after"`
	TLSVersion  string    `json:"tls_version"`
	ALPN        string    `json:"alpn"`
	Curve       string    `json:"curve"`
	Suitable    bool      `json:"suitable"`
	NetworkPath string    `json:"network_path"`
	verified    bool
}

var ErrReality = errors.New("REALITY目标未通过当前VPS的公开IPv4/TLS1.3/X25519/h2及系统证书校验；不会禁用验证或探测其他端口")

// An unauthenticated browser JSON result cannot set verified. Probe performs a
// single fixed-port TLS handshake with one validated numeric IPv4. Never an
// HTTP fetch, private-network scan, insecure retry or target-supplied command.
func ProbeReality(ctx context.Context, host string) (RealityTarget, error) {
	host, e := certassets.Hostname(host)
	if e != nil || !domain.PublicHostname(host) {
		return RealityTarget{}, ErrReality
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, e := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if e != nil || len(addresses) == 0 || len(addresses) > 16 {
		return RealityTarget{}, ErrReality
	}
	for _, ip := range addresses {
		if egress.PublicIPv4(ip.String()) != nil {
			return RealityTarget{}, ErrReality
		}
	}
	address := addresses[0].String()
	conn, e := (&net.Dialer{}).DialContext(ctx, "tcp4", net.JoinHostPort(address, "443"))
	if e != nil {
		return RealityTarget{}, ErrReality
	}
	secured := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}, NextProtos: []string{"h2"}})
	defer secured.Close()
	if e = secured.HandshakeContext(ctx); e != nil {
		return RealityTarget{}, ErrReality
	}
	state := secured.ConnectionState()
	if state.Version != tls.VersionTLS13 || state.NegotiatedProtocol != "h2" || len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 || time.Until(state.PeerCertificates[0].NotAfter) < time.Hour {
		return RealityTarget{}, ErrReality
	}
	return RealityTarget{Hostname: host, Address: address, Port: 443, CheckedAt: time.Now().UTC(), NotAfter: state.PeerCertificates[0].NotAfter, TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true, NetworkPath: "explicit VPS host-network bootstrap TLS probe; not VPN egress", verified: true}, nil
}
func (t RealityTarget) validate(host string) error {
	if !t.verified || !t.Suitable || t.Hostname != host || net.ParseIP(t.Address) == nil || t.Port < 1 || t.Port > 65535 || t.TLSVersion != "1.3" || t.ALPN != "h2" || t.Curve != "X25519" || time.Since(t.CheckedAt) > 10*time.Minute || time.Until(t.CheckedAt) > time.Minute || time.Until(t.NotAfter) < time.Hour {
		return ErrReality
	}
	return nil
}

// NeedsRefresh describes cached metadata only; it never creates a trusted probe
// result. Every explicit REALITY start and recovery still performs a new probe.
func (t RealityTarget) NeedsRefresh(now time.Time) bool {
	return !t.Suitable || egress.PublicIPv4(t.Address) != nil || t.Port != 443 || t.TLSVersion != "1.3" || t.ALPN != "h2" || t.Curve != "X25519" || t.CheckedAt.IsZero() || now.Sub(t.CheckedAt) > 10*time.Minute || t.CheckedAt.Sub(now) > time.Minute || t.NotAfter.Sub(now) < time.Hour
}
