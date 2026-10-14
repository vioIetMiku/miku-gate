// Package management provides explicit direct HTTPS management endpoints.
// A public Origin is not a proxy trust declaration; forwarding headers never
// authorize plaintext access or change the accepted Host/Origin.
package management

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mikugate/internal/certassets"
)

var ErrHTTPS = errors.New("管理HTTPS配置/证书/权限无效；不会改为公网明文或跳过身份检查")

type Endpoint struct {
	Origin    string `json:"origin"`
	Hostname  string `json:"hostname"`
	Authority string `json:"authority"`
	Port      int    `json:"port"`
	IP        bool   `json:"ip"`
}

func ParseOrigin(value string) (Endpoint, error) {
	return parseOrigin(value, "https", 443)
}

// HTTPS certificate and Agent callers keep using ParseOrigin.
func ParsePublicOrigin(value string, allowHTTP bool) (Endpoint, error) {
	if allowHTTP && strings.HasPrefix(value, "http://") {
		return parseOrigin(value, "http", 80)
	}
	return ParseOrigin(value)
}

func parseOrigin(value, scheme string, defaultPort int) (Endpoint, error) {
	u, e := url.Parse(value)
	if e != nil || u.Scheme != scheme || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(value, "\r\n\\ ") {
		return Endpoint{}, ErrHTTPS
	}
	host := u.Hostname()
	if host == "" {
		return Endpoint{}, ErrHTTPS
	}
	isIP := false
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() {
			return Endpoint{}, ErrHTTPS
		}
		host = ip.String()
		isIP = true
	} else {
		host, e = certassets.Hostname(host)
		if e != nil {
			return Endpoint{}, ErrHTTPS
		}
	}
	port := defaultPort
	if u.Port() != "" {
		port, e = strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return Endpoint{}, ErrHTTPS
		}
	}
	authority := host
	if isIP && strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != defaultPort {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	}
	// Canonical origin avoids URL userinfo, aliases, duplicate colons, trailing
	// dots and host/port spellings turning into a second privileged endpoint.
	canonical := scheme + "://" + authority
	if value != canonical {
		return Endpoint{}, ErrHTTPS
	}
	return Endpoint{canonical, host, authority, port, isIP}, nil
}

type CertificateInfo struct {
	Origin             string    `json:"origin"`
	LeafSHA256         string    `json:"leaf_sha256"`
	NotAfter           time.Time `json:"not_after"`
	SystemTrusted      bool      `json:"system_trusted"`
	Bootstrap          bool      `json:"bootstrap"`
	PrivateKeyReturned bool      `json:"private_key_returned"`
}

func Load(endpoint Endpoint, certPath, keyPath string, allowBootstrap bool) (*tls.Config, CertificateInfo, error) {
	info := CertificateInfo{Origin: endpoint.Origin}
	checked, e := ParseOrigin(endpoint.Origin)
	if e != nil || checked != endpoint || certPath == keyPath {
		return nil, info, ErrHTTPS
	}
	cert, e := readPEM(certPath, false)
	if e != nil {
		return nil, info, ErrHTTPS
	}
	key, e := readPEM(keyPath, true)
	if e != nil {
		return nil, info, ErrHTTPS
	}
	defer clear(key)
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil || len(pair.Certificate) == 0 || len(pair.Certificate) > 8 {
		return nil, info, ErrHTTPS
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || leaf.IsCA || leaf.VerifyHostname(endpoint.Hostname) != nil || leaf.NotBefore.After(time.Now()) || time.Until(leaf.NotAfter) < 24*time.Hour {
		return nil, info, ErrHTTPS
	}
	switch pub := leaf.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if pub.Curve.Params().BitSize < 256 {
			return nil, info, ErrHTTPS
		}
	case *rsa.PublicKey:
		if pub.N.BitLen() < 2048 {
			return nil, info, ErrHTTPS
		}
	default:
		return nil, info, ErrHTTPS
	}
	switch leaf.SignatureAlgorithm {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.MD5WithRSA, x509.DSAWithSHA1:
		return nil, info, ErrHTTPS
	}
	pool := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		parsed, e := x509.ParseCertificate(raw)
		if e != nil {
			return nil, info, ErrHTTPS
		}
		pool.AddCert(parsed)
	}
	_, e = leaf.Verify(x509.VerifyOptions{DNSName: endpoint.Hostname, Intermediates: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	info.SystemTrusted = e == nil
	if e != nil {
		if !allowBootstrap || !endpoint.IP || len(pair.Certificate) != 1 || !bytes.Equal(leaf.RawSubject, leaf.RawIssuer) || leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) != nil {
			return nil, info, ErrHTTPS
		}
		info.Bootstrap = true
	}
	sum := sha256.Sum256(leaf.Raw)
	info.LeafSHA256 = hex.EncodeToString(sum[:])
	info.NotAfter = leaf.NotAfter
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}, info, nil
}

// BootstrapIP is explicit, local and exclusive. It does not establish public
// CA trust; users must verify the printed fingerprint out-of-band/import it.
func BootstrapIP(host, dir string) (CertificateInfo, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return CertificateInfo{}, ErrHTTPS
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return CertificateInfo{}, ErrHTTPS
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return CertificateInfo{}, ErrHTTPS
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Miku Gate explicit IP bootstrap"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(90 * 24 * time.Hour), IPAddresses: []net.IP{ip}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	raw, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return CertificateInfo{}, ErrHTTPS
	}
	encoded, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return CertificateInfo{}, ErrHTTPS
	}
	defer clear(encoded)
	if e = writeBootstrap(dir, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})); e != nil {
		return CertificateInfo{}, e
	}
	sum := sha256.Sum256(raw)
	authority := ip.String()
	if ip.To4() == nil {
		authority = "[" + authority + "]"
	}
	return CertificateInfo{Origin: "https://" + authority, LeafSHA256: hex.EncodeToString(sum[:]), NotAfter: template.NotAfter, Bootstrap: true}, nil
}

// ValidateFiles is read-only, deliberately doesn't write or export PEM.
func ValidateFiles(origin, cert, key string, bootstrap bool) (CertificateInfo, error) {
	endpoint, e := ParseOrigin(origin)
	if e != nil {
		return CertificateInfo{}, e
	}
	_, summary, e := Load(endpoint, cert, key, bootstrap)
	return summary, e
}
