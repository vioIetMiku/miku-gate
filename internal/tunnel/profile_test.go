package tunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

type testIdentity struct{ ca, clientCert, clientKey, serverCert, serverKey string }

func testPKI(t testing.TB) testIdentity {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Miku isolated test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(2 * time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	pair := func(name string, serial int64, usage x509.ExtKeyUsage) (string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
	}
	clientCert, clientKey := pair("miku-lab-client", 2, x509.ExtKeyUsageClientAuth)
	serverCert, serverKey := pair("miku-lab-server", 3, x509.ExtKeyUsageServerAuth)
	return testIdentity{ca: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})), clientCert: clientCert, clientKey: clientKey, serverCert: serverCert, serverKey: serverKey}
}
func (p testIdentity) profile(ip string) string {
	return "client\ndev tun\nproto udp4\nremote " + ip + " 11940\nremote-cert-tls server\nverify-x509-name miku-lab-server name\n<ca>\n" + p.ca + "</ca>\n<cert>\n" + p.clientCert + "</cert>\n<key>\n" + p.clientKey + "</key>\n"
}
func TestNormalizeReconstructsOwnedConfiguration(t *testing.T) {
	pki := testPKI(t)
	raw := pki.profile("8.8.8.8") + "redirect-gateway def1 bypass-dhcp\nroute-delay 12\nverb 8\nauth SHA512\ncomp-lzo no\n"
	profile, err := Normalize(raw, "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	text := profile.render()
	for _, required := range []string{"dev " + Device, "proto udp4", "route-nopull", "route-noexec", "tls-version-min 1.2", "remote-cert-tls server", "allow-compression no", "disable-dco", "script-security 1", "pull-filter ignore \"dhcp-option\""} {
		if !strings.Contains(text, required+"\n") {
			t.Fatal("missing fixed safety option", required)
		}
	}
	for _, omitted := range []string{"route-delay 12", "verb 8", "comp-lzo no", "redirect-gateway def1", "auth SHA512"} {
		if strings.Contains(text, omitted) {
			t.Fatal("source setting was blindly forwarded", omitted)
		}
	}
	if endpoint := profile.Endpoint(); endpoint.IP.String() != "8.8.8.8" || endpoint.Port != 11940 || endpoint.Transport != "udp4" {
		t.Fatal(endpoint)
	}
	data, _ := json.Marshal(profile)
	for _, exposed := range []string{string(data), fmt.Sprint(profile), fmt.Sprintf("%+v", profile), fmt.Sprintf("%#v", profile)} {
		if strings.Contains(exposed, "PRIVATE KEY") || strings.Contains(exposed, pki.ca) {
			t.Fatal("profile credentials appeared in ordinary logging")
		}
	}
}
func TestNormalizeRejectsAmbiguousOrUnsafeInput(t *testing.T) {
	pki := testPKI(t)
	valid := pki.profile("8.8.8.8")
	attacks := map[string]string{
		"script": valid + "up /bin/sh\n", "plugin": valid + "plugin /tmp/evil.so\n", "include": valid + "config /tmp/evil\n", "auth-file": valid + "auth-user-pass /etc/shadow\n", "interactive-auth": valid + "auth-user-pass\n",
		"remote-duplicate": valid + "remote 8.8.8.8 11940\n", "different-endpoint": strings.Replace(valid, "remote 8.8.8.8", "remote 1.1.1.1", 1), "quoted-argument": strings.Replace(valid, "remote 8.8.8.8", `remote "8.8.8.8"`, 1),
		"compression": valid + "comp-lzo yes\n", "weak-cipher": valid + "cipher AES-128-CBC\n", "weak-auth": valid + "auth SHA1\n", "bare-ca": strings.Replace(valid, pki.ca, "not a certificate", 1), "duplicate-ca": valid + "<ca>\n" + pki.ca + "</ca>\n",
		"wrong-key": strings.Replace(valid, pki.clientKey, pki.serverKey, 1), "wrong-eku": strings.Replace(strings.Replace(valid, pki.clientCert, pki.serverCert, 1), pki.clientKey, pki.serverKey, 1), "server-ca-as-client": strings.Replace(valid, pki.clientCert, pki.ca, 1),
		"mismatched-proto": strings.Replace(valid, "remote 8.8.8.8 11940", "remote 8.8.8.8 11940 tcp", 1), "missing-dev": strings.Replace(valid, "dev tun\n", "", 1), "remote-port-overflow": strings.Replace(valid, "11940", "65536", 1), "environment": valid + "setenv LD_PRELOAD /tmp/evil\n",
		"nul": valid + "\x00", "cr": valid + "\r", "large": strings.Repeat("a", MaxProfileBytes+1), "pem-prefix": strings.Replace(valid, pki.ca, "garbage\n"+pki.ca, 1), "path-ca": valid + "ca /etc/ssl/certs/ca-certificates.crt\n",
	}
	for name, raw := range attacks {
		t.Run(name, func(t *testing.T) {
			if _, err := Normalize(raw, "8.8.8.8"); err == nil {
				t.Fatal("unsafe profile accepted")
			} else if err != ErrProfile {
				t.Fatal("parser disclosed source details", err)
			}
		})
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "198.18.48.1", "::ffff:8.8.8.8", "8.8.8.8 ", "2001:4860:4860::8888"} {
		if _, err := Normalize(strings.ReplaceAll(valid, "8.8.8.8", ip), ip); err == nil {
			t.Fatal("non-public literal runtime endpoint accepted", ip)
		}
	}
	if _, err := normalize(valid, "8.8.8.8", time.Now().Add(24*time.Hour), func(string) error { return nil }); err == nil {
		t.Fatal("expired test CA was accepted")
	}
}
func FuzzProfileNormalization(f *testing.F) {
	pki := testPKI(f)
	f.Add(pki.profile("8.8.8.8"))
	f.Add("client\ndev tun\nremote 8.8.8.8 443\n")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > MaxProfileBytes+1 {
			return
		}
		p, err := Normalize(raw, "8.8.8.8")
		if err == nil {
			if !strings.Contains(p.render(), "route-nopull\n") || p.Endpoint().IP.String() != "8.8.8.8" {
				t.Fatal("normalization escaped invariants")
			}
		}
	})
}
