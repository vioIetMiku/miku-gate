package vpngate

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strconv"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/egress"
	"mikugate/internal/tunnel"
)

var ErrIdentity = errors.New("无法用官方域名和系统CA验证此节点的TLS身份；未跳过校验，也未启动VPN")
var officialLabel = regexp.MustCompile(`^vpn[0-9]{1,20}(\.opengw\.net)?$`)
var commonName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func OfficialTLSHostname(hostname string) (string, error) {
	if !officialLabel.MatchString(hostname) {
		return "", ErrIdentity
	}
	if suffix := ".opengw.net"; len(hostname) >= len(suffix) && hostname[len(hostname)-len(suffix):] == suffix {
		return hostname, nil
	}
	return hostname + ".opengw.net", nil
}
func ProfileDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// InspectIdentity opens at most one TCP/TLS connection to the cached literal IP.
// TCP OpenVPN uses its advertised port; UDP nodes use the standard SoftEther TLS
// listener at 443. Failure never triggers a port sweep or insecure retry.
func InspectIdentity(ctx context.Context, node domain.Node) (domain.NodeIdentity, error) {
	return inspectIdentity(ctx, node, nil, nil)
}
func inspectIdentity(ctx context.Context, node domain.Node, roots *x509.CertPool, dial func(context.Context, string, string) (net.Conn, error)) (domain.NodeIdentity, error) {
	var result domain.NodeIdentity
	if node.Source != Source || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute || egress.PublicIPv4(node.IP) != nil {
		return result, ErrIdentity
	}
	hostname, err := OfficialTLSHostname(node.Hostname)
	if err != nil {
		return result, err
	}
	endpoint, err := tunnel.InspectionEndpoint(node.Config, node.IP)
	if err != nil {
		return result, ErrIdentity
	}
	port := endpoint.Port
	if endpoint.Transport == "udp4" {
		port = 443
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if dial == nil {
		dial = (&net.Dialer{Timeout: 3 * time.Second}).DialContext
	}
	conn, err := dial(bound, "tcp4", net.JoinHostPort(node.IP, strconv.Itoa(int(port))))
	if err != nil {
		return result, ErrIdentity
	}
	defer conn.Close()
	secured := tls.Client(conn, &tls.Config{ServerName: hostname, RootCAs: roots, MinVersion: tls.VersionTLS12})
	defer secured.Close()
	stop := context.AfterFunc(bound, func() { secured.Close() })
	defer stop()
	if err = secured.HandshakeContext(bound); err != nil {
		return result, ErrIdentity
	}
	state := secured.ConnectionState()
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		return result, ErrIdentity
	}
	leaf := state.PeerCertificates[0]
	if !commonName.MatchString(leaf.Subject.CommonName) || leaf.IsCA {
		return result, ErrIdentity
	}
	keyOK := false
	switch key := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		keyOK = key.N.BitLen() >= 2048
	case *ecdsa.PublicKey:
		keyOK = key.Curve.Params().BitSize >= 256
	case ed25519.PublicKey:
		keyOK = len(key) == ed25519.PublicKeySize
	}
	if !keyOK {
		return result, ErrIdentity
	}
	digest := sha256.Sum256(leaf.Raw)
	result = domain.NodeIdentity{NodeID: node.ID, IP: node.IP, TLSHost: hostname, TLSPort: port, PeerName: leaf.Subject.CommonName, PeerFingerprint: hex.EncodeToString(digest[:]), ProfileSHA256: ProfileDigest(node.Config), TLSVersion: tls.VersionName(state.Version), CheckedAt: time.Now().UTC(), NotAfter: leaf.NotAfter.UTC(), Source: Source, Verified: true, Message: "系统CA与官方域名TLS校验已通过。将固定本次证书指纹，公共节点可能共享证书；这不是VPN连通、住宅或运营者信誉证明。"}
	return result, nil
}
