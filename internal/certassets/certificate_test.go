package certassets

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) ([]byte, []byte, *x509.CertPool, time.Time) {
	t.Helper()
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private test root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(7 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caRaw, e := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	parsed, _ := x509.ParseCertificate(caRaw)
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"entry.example"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(72 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), roots, now
}
func TestCertificateTrustNameValidityKeyAndNoSecretSerialization(t *testing.T) {
	cert, key, roots, now := fixture(t)
	asset, e := validate("entry.example", cert, key, roots, now)
	if e != nil {
		t.Fatal(e)
	}
	summary := asset.Summary()
	if !summary.ChainVerified || !summary.KeyMatched || len(summary.LeafSHA256) != 64 {
		t.Fatal("missing actual checks")
	}
	raw, _ := json.Marshal(asset)
	if strings.Contains(string(raw), "PRIVATE KEY") || bytes.Contains(raw, key) {
		t.Fatal("asset encoding leaks key")
	}
	copiedCert, copiedKey := asset.PEM()
	copiedCert[0] = 'x'
	copiedKey[0] = 'x'
	c, k := asset.PEM()
	if !bytes.Equal(c, cert) || !bytes.Equal(k, key) {
		t.Fatal("asset alias mutation")
	}
	_, wrongKey, _, _ := fixture(t)
	for _, tc := range []struct {
		host      string
		cert, key []byte
		roots     *x509.CertPool
		now       time.Time
	}{{"wrong.example", cert, key, roots, now}, {"entry.example", cert, wrongKey, roots, now}, {"entry.example", cert, key, x509.NewCertPool(), now}, {"entry.example", cert, key, roots, now.Add(60 * time.Hour)}, {"entry.example", append(cert, key...), key, roots, now}, {"entry.example", bytes.Repeat([]byte("x"), MaxPEM+1), key, roots, now}} {
		if _, e := validate(tc.host, tc.cert, tc.key, tc.roots, tc.now); e != ErrCertificate {
			t.Fatal("unsafe pair accepted or raw error leak")
		}
	}
}
func TestHostnameIDNAAndPathRejection(t *testing.T) {
	for _, bad := range []string{"/etc/shadow", "../example.com", "example.com:443", "127.0.0.1", "*.example.com", "entry.example/", "entry.example.", "a..example", " localhost", "metadata", "entry.example\n"} {
		if _, e := Hostname(bad); e == nil {
			t.Fatalf("unsafe hostname %q", bad)
		}
	}
	if host, e := Hostname("例子.测试"); e != nil || !strings.HasPrefix(host, "xn--") {
		t.Fatal("IDNA hostname not normalized")
	}
}
