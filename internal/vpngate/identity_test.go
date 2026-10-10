package vpngate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"mikugate/internal/domain"
	"net"
	"strings"
	"testing"
	"time"
)

func identityFixture(t *testing.T, dns string) (domain.Node, *x509.CertPool, tls.Certificate, string) {
	t.Helper()
	now := time.Now()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private identity test root"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(der)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "opengw.net"}, DNSNames: []string{dns}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	cert, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := x509.MarshalPKCS8PrivateKey(leafKey)
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}))
	if err != nil {
		t.Fatal(err)
	}
	caText := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caText)
	hash := sha256.Sum256(cert)
	return domain.Node{ID: "identity-fixture", Hostname: "vpn123", IP: "8.8.8.8", Source: Source, FetchedAt: now, Config: "client\ndev tun\nproto tcp4-client\nremote 8.8.8.8 1448\n<ca>\n" + string(caText) + "</ca>\n"}, roots, pair, hex.EncodeToString(hash[:])
}
func TestOfficialIdentityUsesVerifiedSANAndPinsLeaf(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid-wildcard", false: "wrong-name"}[valid], func(t *testing.T) {
			dns := "*.opengw.net"
			if !valid {
				dns = "other.example"
			}
			node, roots, cert, want := identityFixture(t, dns)
			listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err == nil {
					defer conn.Close()
					conn.(*tls.Conn).Handshake()
				}
			}()
			var calls int
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				calls++
				if network != "tcp4" || address != "8.8.8.8:1448" {
					t.Error("identity lookup changed endpoint or used host DNS", network, address)
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp4", listener.Addr().String())
			}
			identity, err := inspectIdentity(t.Context(), node, roots, dial)
			<-done
			if !valid {
				if err == nil {
					t.Fatal("wrong TLS name accepted")
				}
				return
			}
			if err != nil || !identity.Verified || identity.VPNConnected || identity.PeerName != "opengw.net" || identity.PeerFingerprint != want || identity.TLSHost != "vpn123.opengw.net" || identity.ProfileSHA256 != ProfileDigest(node.Config) || calls != 1 {
				t.Fatal("incomplete verified identity", identity, err)
			}
		})
	}
}
func TestIdentityNeverProbesPrivateStaleOrUntrustedNode(t *testing.T) {
	node, roots, _, _ := identityFixture(t, "*.opengw.net")
	for _, kind := range []string{"private", "stale", "source", "name", "future"} {
		copy := node
		switch kind {
		case "private":
			copy.IP = "127.0.0.1"
		case "stale":
			copy.FetchedAt = time.Now().Add(-25 * time.Hour)
		case "source":
			copy.Source = "http://untrusted.invalid"
		case "name":
			copy.Hostname = "vpn123.opengw.net.evil.example"
		case "future":
			copy.FetchedAt = time.Now().Add(time.Hour)
		}
		if _, err := inspectIdentity(t.Context(), copy, roots, func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("unsafe identity endpoint contacted", kind)
			return nil, nil
		}); err == nil {
			t.Fatal("unsafe metadata accepted", kind)
		}
	}
	for _, name := range []string{"localhost", "127.0.0.1", "vpn123.opengw.net.evil", "VPN123", "vpn12/34", "vpn123\n"} {
		if _, err := OfficialTLSHostname(name); err == nil {
			t.Fatal("invalid official name", name)
		}
	}
	for _, name := range []string{"vpn123", "vpn123.opengw.net"} {
		got, err := OfficialTLSHostname(name)
		if err != nil || got != "vpn123.opengw.net" {
			t.Fatal(name, got, err)
		}
	}
	if strings.Contains(ErrIdentity.Error(), "PRIVATE KEY") {
		t.Fatal("credential material in error")
	}
}
