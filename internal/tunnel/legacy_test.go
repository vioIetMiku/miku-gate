package tunnel

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestVPNGateLegacyRequiresExplicitConsentAndPeerName(t *testing.T) {
	pki := testPKI(t)
	raw := pki.profile("8.8.8.8") + "cipher AES-128-CBC\ndata-ciphers AES-128-CBC\nauth SHA1\n"
	if _, err := Normalize(raw, "8.8.8.8"); err == nil {
		t.Fatal("default strict policy silently weakened")
	}
	for _, options := range []NormalizeOptions{{}, {AllowVPNGateLegacy: true}, {ExpectedPeerName: "miku-lab-server"}, {AllowVPNGateLegacy: true, ExpectedPeerName: "other-peer"}, {AllowVPNGateLegacy: true, ExpectedPeerName: "name\nscript-security 3"}} {
		if _, err := NormalizeWithOptions(raw, "8.8.8.8", options); err == nil {
			t.Fatal("unsafe/ambiguous consent or identity accepted", options)
		}
	}
	profile, err := NormalizeWithOptions(raw, "8.8.8.8", NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: "miku-lab-server"})
	if err != nil {
		t.Fatal(err)
	}
	if !profile.LegacyDataChannel() || profile.SharedClientIdentity() {
		t.Fatal("misleading compatibility mode")
	}
	output := profile.render()
	for _, required := range []string{"tls-version-min 1.2\n", "remote-cert-tls server\n", "verify-x509-name miku-lab-server name\n", "data-ciphers-fallback AES-128-CBC\n", "auth SHA1\n", "script-security 1\n", "route-nopull\n", "route-noexec\n", "allow-compression no\n"} {
		if !strings.Contains(output, required) {
			t.Fatal("missing safety/compatibility option", required)
		}
	}
	for _, forbidden := range []string{"tls-cert-profile insecure", "tls-cipher DEFAULT:@SECLEVEL=0", "auth-user-pass", "script-security 2", "\ncompress "} {
		if strings.Contains(output, forbidden) {
			t.Fatal("server TLS or script policy weakened", forbidden)
		}
	}
}
func TestCompatibilityDoesNotAllowArbitraryLegacySettings(t *testing.T) {
	pki := testPKI(t)
	valid := pki.profile("8.8.8.8")
	options := NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: "miku-lab-server"}
	for _, suffix := range []string{"cipher BF-CBC\n", "cipher DES-EDE3-CBC\n", "data-ciphers AES-256-CBC\n", "tls-version-min 1.0\n", "tls-cert-profile insecure\n", "tls-cipher DEFAULT:@SECLEVEL=0\n", "comp-lzo yes\n", "compress lzo\n", "plugin /tmp/evil.so\n", "up /bin/sh\n", "auth-user-pass\n"} {
		if _, err := NormalizeWithOptions(valid+suffix, "8.8.8.8", options); err == nil {
			t.Fatal("unsupported directive enabled by legacy mode", suffix)
		}
	}
	profile, err := NormalizeWithOptions(valid, "8.8.8.8", options)
	if err != nil {
		t.Fatal(err)
	}
	if profile.LegacyDataChannel() || strings.Contains(profile.render(), "AES-128-CBC") {
		t.Fatal("opt-in downgraded an already-modern profile")
	}
}
func TestSharedClientExceptionIsExactNotAnySHA1Certificate(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(39), Subject: pkix.Name{CommonName: "VPNGateClientCert"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), SignatureAlgorithm: x509.SHA1WithRSA}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	secret := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if _, _, err := normalizeVPNGateSharedClient(cert, secret, now); err == nil {
		t.Fatal("same subject name bypassed exact fingerprint pin")
	}
	pki := testPKI(t)
	raw := strings.Replace(strings.Replace(pki.profile("8.8.8.8"), pki.clientCert, cert, 1), pki.clientKey, secret, 1)
	if _, err := NormalizeWithOptions(raw, "8.8.8.8", NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: "miku-lab-server"}); err == nil {
		t.Fatal("unrelated weak client identity accepted")
	}
}
func FuzzNormalizeLegacy(f *testing.F) {
	pki := testPKI(f)
	f.Add(pki.profile("8.8.8.8") + "cipher AES-128-CBC\nauth SHA1\n")
	f.Add("client\ndev tun\nremote 8.8.8.8 443\nplugin /bin/sh\n")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > MaxProfileBytes+1 {
			return
		}
		profile, err := NormalizeWithOptions(raw, "8.8.8.8", NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: "miku-lab-server"})
		if err == nil {
			rendered := profile.render()
			if !strings.Contains(rendered, "tls-version-min 1.2\n") || !strings.Contains(rendered, "script-security 1\n") || !strings.Contains(rendered, "verify-x509-name miku-lab-server name\n") {
				t.Fatal("legacy parser escaped fixed safety policy")
			}
		}
	})
}
