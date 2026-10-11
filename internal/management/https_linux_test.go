//go:build linux

package management

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapPrivacyTrustNameAndExclusiveIdentity(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	dir := filepath.Join(root, "https")
	s, e := BootstrapIP("127.0.0.1", dir)
	if e != nil {
		t.Fatal(e)
	}
	if s.SystemTrusted || !s.Bootstrap || s.PrivateKeyReturned || len(s.LeafSHA256) != 64 {
		t.Fatal("false trust")
	}
	cp, kp := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "private.key")
	cert, _ := os.ReadFile(cp)
	b, _ := pem.Decode(cert)
	leaf, e := x509.ParseCertificate(b.Bytes)
	if e != nil || leaf.VerifyHostname("127.0.0.1") != nil {
		t.Fatal("missing IP SAN")
	}
	ep, _ := ParseOrigin("https://127.0.0.1:8443")
	if _, _, e := Load(ep, cp, kp, false); e == nil {
		t.Fatal("silent bootstrap trust")
	}
	conf, info, e := Load(ep, cp, kp, true)
	if e != nil || conf.MinVersion != tls.VersionTLS12 || info.SystemTrusted || !info.Bootstrap {
		t.Fatal("explicit bootstrap", e)
	}
	wrong, _ := ParseOrigin("https://192.0.2.20:8443")
	if _, _, e := Load(wrong, cp, kp, true); e == nil {
		t.Fatal("wrong SAN")
	}
	if _, e := BootstrapIP("127.0.0.1", dir); e == nil {
		t.Fatal("overwritten")
	}
	after, _ := os.ReadFile(cp)
	if !bytes.Equal(cert, after) {
		t.Fatal("identity changed")
	}
	for _, p := range []string{cp, kp} {
		i, _ := os.Stat(p)
		if i.Mode().Perm() != 0600 {
			t.Fatal("private file")
		}
	}
	os.Chmod(kp, 0644)
	if _, _, e := Load(ep, cp, kp, true); e == nil {
		t.Fatal("world readable key")
	}
	os.Chmod(kp, 0600)
	alias := filepath.Join(root, "alias")
	os.Symlink(dir, alias)
	if _, _, e := Load(ep, filepath.Join(alias, "certificate.pem"), filepath.Join(alias, "private.key"), true); e == nil {
		t.Fatal("symlink parent")
	}
	if _, e := BootstrapIP("entry.example", filepath.Join(root, "dns")); e == nil {
		t.Fatal("domain selfsigned")
	}
}
