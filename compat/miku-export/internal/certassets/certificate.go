// Package certassets validates local TLS assets without exposing private keys.
package certassets

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"golang.org/x/net/idna"
	"net"
	"strings"
	"time"
)

const MaxPEM = 128 << 10

var ErrCertificate = errors.New("证书域名/有效期/信任链/算法或公私钥匹配未通过；不回显证书私钥或原始内容")

type Summary struct {
	Hostname      string    `json:"hostname"`
	LeafSHA256    string    `json:"leaf_sha256"`
	NotBefore     time.Time `json:"not_before"`
	NotAfter      time.Time `json:"not_after"`
	ChainVerified bool      `json:"chain_verified"`
	KeyMatched    bool      `json:"key_matched"`
}

// Asset has no exported secret field, even accidental JSON encoding is safe.
// PEM copies are obtained only by local file orchestration, never HTTP output.
type Asset struct {
	summary   Summary
	cert, key []byte
}

func (a *Asset) Summary() Summary {
	if a == nil {
		return Summary{}
	}
	return a.summary
}
func (a *Asset) PEM() ([]byte, []byte) {
	if a == nil {
		return nil, nil
	}
	return bytes.Clone(a.cert), bytes.Clone(a.key)
}
func (a *Asset) MarshalJSON() ([]byte, error) { return json.Marshal(a.Summary()) }
func Hostname(value string) (string, error) {
	if value != strings.TrimSpace(value) || len(value) > 253 || strings.ContainsAny(value, "/\\:@\x00\r\n *") {
		return "", ErrCertificate
	}
	host, e := idna.Lookup.ToASCII(strings.ToLower(value))
	if e != nil || len(host) < 3 || len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return "", ErrCertificate
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrCertificate
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrCertificate
			}
		}
	}
	return host, nil
}

// Validate requires the system roots. Tests inject a private root only through
// the package-local verifier; production cannot toggle off certificate trust.
func Validate(host string, cert, key []byte) (*Asset, error) {
	return validate(host, cert, key, nil, time.Now())
}
func validate(host string, cert, key []byte, roots *x509.CertPool, now time.Time) (*Asset, error) {
	host, e := Hostname(host)
	if e != nil || len(cert) == 0 || len(key) == 0 || len(cert) > MaxPEM || len(key) > MaxPEM {
		return nil, ErrCertificate
	}
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil || len(pair.Certificate) == 0 || len(pair.Certificate) > 8 {
		return nil, ErrCertificate
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || leaf.IsCA || leaf.NotAfter.Sub(now) < 24*time.Hour || leaf.NotBefore.After(now) {
		return nil, ErrCertificate
	}
	switch pub := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		if pub.N.BitLen() < 2048 {
			return nil, ErrCertificate
		}
	case *ecdsa.PublicKey:
		if pub.Curve.Params().BitSize < 256 {
			return nil, ErrCertificate
		}
	case ed25519.PublicKey:
	default:
		return nil, ErrCertificate
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, ErrCertificate
	}
	public, e := x509.MarshalPKIXPublicKey(signer.Public())
	if e != nil {
		return nil, ErrCertificate
	}
	want, e := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if e != nil || !bytes.Equal(public, want) {
		return nil, ErrCertificate
	}
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		ca, e := x509.ParseCertificate(raw)
		if e != nil {
			return nil, ErrCertificate
		}
		intermediates.AddCert(ca)
	}
	chains, e := leaf.Verify(x509.VerifyOptions{DNSName: host, CurrentTime: now, Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	if e != nil || len(chains) == 0 {
		return nil, ErrCertificate
	}
	for _, chain := range chains {
		for _, c := range chain {
			switch c.SignatureAlgorithm {
			case x509.MD2WithRSA, x509.MD5WithRSA, x509.SHA1WithRSA, x509.DSAWithSHA1, x509.ECDSAWithSHA1:
				return nil, ErrCertificate
			}
		}
	}
	keyBlock, keyRest := pem.Decode(key)
	if keyBlock == nil || len(bytes.TrimSpace(keyRest)) != 0 || len(keyBlock.Headers) > 0 {
		return nil, ErrCertificate
	}
	switch keyBlock.Type {
	case "PRIVATE KEY", "EC PRIVATE KEY", "RSA PRIVATE KEY":
	default:
		return nil, ErrCertificate
	}
	// Reject extraneous non-certificate blocks rather than embedding hidden keys
	// from a misconfigured fullchain file into a public certificate asset.
	rest := cert
	for len(bytes.TrimSpace(rest)) > 0 {
		block, next := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) > 0 {
			return nil, ErrCertificate
		}
		rest = next
	}
	sum := sha256.Sum256(leaf.Raw)
	return &Asset{summary: Summary{host, hex.EncodeToString(sum[:]), leaf.NotBefore, leaf.NotAfter, true, true}, cert: bytes.Clone(cert), key: bytes.Clone(key)}, nil
}

type Finding struct {
	Provider    string   `json:"provider"`
	State       string   `json:"state"`
	Certificate *Summary `json:"certificate,omitempty"`
}
type Discovery struct {
	Hostname           string    `json:"hostname"`
	Findings           []Finding `json:"findings"`
	PrivateKeyReturned bool      `json:"private_key_returned"`
	SelectedProvider   string    `json:"selected_provider,omitempty"`
}
