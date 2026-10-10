package store

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"mikugate/internal/vpngate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func vpnNode(t *testing.T) domain.Node {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(10), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}))
	return domain.Node{ID: "test-vpn-node", IP: "8.8.8.8", Hostname: "vpn.example", Source: vpngate.Source, Country: "JP", FetchedAt: now, Config: "client\ndev tun\nproto udp4\nremote 8.8.8.8 1194\n<ca>\n" + ca + "</ca>\n"}
}
func TestVPNBindingIsEncryptedSnapshotAndModeSwitchIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	exit, _, err := s.CreateExit(domain.ExitInput{Name: "VPN", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	node := vpnNode(t)
	if err = s.ReplaceNodes(t.Context(), []domain.Node{node}); err != nil {
		t.Fatal(err)
	}
	input := domain.VPNInput{NodeID: node.ID, PeerName: "vpn.example", Confirm: true}
	denied := input
	denied.Confirm = false
	if s.SaveVPN(exit.ID, denied) == nil {
		t.Fatal("implicit selection allowed")
	}
	if err = s.SaveVPN(exit.ID, input); err != nil {
		t.Fatal(err)
	}
	config, ok, err := s.VPN(exit.ID)
	if err != nil || !ok || config.Profile != node.Config || config.PeerName != input.PeerName {
		t.Fatal("snapshot read failed", err)
	}
	raw, _ := json.Marshal(config)
	if bytes.Contains(raw, []byte("BEGIN CERTIFICATE")) || bytes.Contains(raw, []byte(node.Config)) {
		t.Fatal("profile leaked to API")
	}
	var encrypted []byte
	s.db.QueryRow("SELECT sealed FROM vpn_configs WHERE exit_id=?", exit.ID).Scan(&encrypted)
	if bytes.Contains(encrypted, []byte("BEGIN CERTIFICATE")) {
		t.Fatal("plaintext config in binding")
	}
	originalHash := sha256.Sum256([]byte(node.Config))
	if config.ProfileSHA256 != hex.EncodeToString(originalHash[:]) {
		t.Fatal("wrong snapshot digest")
	}
	node.Config = "changed and untrusted source"
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	config, _, err = s.VPN(exit.ID)
	if err != nil || config.ProfileSHA256 != hex.EncodeToString(originalHash[:]) {
		t.Fatal("cache refresh rewrote an authorized snapshot")
	}
	rows, _ := s.ListExits()
	if len(rows) != 1 || rows[0].UpstreamKind != "openvpn" {
		t.Fatal("VPN mode not displayed")
	}
	upstream := domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443}
	if err = s.SaveUpstream(exit.ID, upstream); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("VPN silently replaced by proxy", err)
	}
	upstream.ReplaceVPN = true
	if err = s.SaveUpstream(exit.ID, upstream); err != nil {
		t.Fatal(err)
	}
	if _, exists, _ := s.VPN(exit.ID); exists {
		t.Fatal("conflicting modes retained")
	}
	node = vpnNode(t)
	s.ReplaceNodes(t.Context(), []domain.Node{node})
	if err = s.SaveVPN(exit.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, _, exists, _ := s.Upstream(exit.ID); exists {
		t.Fatal("proxy retained when explicitly selecting VPN")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if config, ok, err = s.VPN(exit.ID); err != nil || !ok || config.Profile != node.Config {
		t.Fatal("restart lost encrypted binding", err)
	}
	s.Close()
	if err = os.Remove(path + ".key"); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path); !errors.Is(err, secretstore.ErrKey) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("missing VPN key silently recreated", err)
	}
}
func TestVPNSelectionRejectsStaleOrNonOfficialCache(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	exit, _, _ := s.CreateExit(domain.ExitInput{Name: "test", Country: "JP"})
	for _, kind := range []string{"stale", "source"} {
		node := vpnNode(t)
		if kind == "stale" {
			node.FetchedAt = time.Now().Add(-25 * time.Hour)
		} else {
			node.Source = "https://example.invalid"
		}
		s.ReplaceNodes(t.Context(), []domain.Node{node})
		if s.SaveVPN(exit.ID, domain.VPNInput{NodeID: node.ID, PeerName: "vpn.example", Confirm: true}) == nil {
			t.Fatal("unsafe cached source accepted", kind)
		}
	}
}
