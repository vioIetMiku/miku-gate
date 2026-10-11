package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/secretstore"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func managedFixture(exit domain.Exit, password string, port int) ingress.ManagedMaterial {
	return ingress.ManagedMaterial{Request: ingress.ManagedRequest{Core: "sing-box", Template: "vless-reality-vision", Address: "entry.example", Listen: "0.0.0.0", Port: port, ServerName: "target.example", RealityHostname: "target.example"}, Proxy: ingress.Proxy{Port: exit.Port, Username: exit.Username, Password: password}, Identity: ingress.ManagedIdentity{UUID: "7c664910-2503-4f16-853f-636c5c68ff90", Password: strings.Repeat("a", 32), RealityPrivateKey: "aRlwIfRYFL8CDWSaxjwjorGXIZAzTy9hUXGgBFfltmI", ShortID: "071c5e3d4b09ab5e"}, Target: &ingress.RealityTarget{Hostname: "target.example", Address: "8.8.8.8", Port: 443, CheckedAt: time.Now(), NotAfter: time.Now().Add(24 * time.Hour), TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true}}
}
func TestManagedEncryptedPersistenceBindingAndMissingKeyFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	exit, password, e := s.CreateExit(domain.ExitInput{Name: "managed", Country: "JP", Port: 22000})
	if e != nil {
		t.Fatal(e)
	}
	_, hash, _ := s.ExitForRuntime(exit.ID)
	m := managedFixture(exit, password, 25001)
	if e = s.SaveManaged(exit.ID, m, hash); e != nil {
		t.Fatal(e)
	}
	view, e := s.ManagedView(exit.ID)
	if e != nil || !view.Configured || view.Applied {
		t.Fatal("wrong durable view")
	}
	safe, _ := json.Marshal(view)
	for _, secret := range []string{password, m.Identity.UUID, m.Identity.RealityPrivateKey} {
		if bytes.Contains(safe, []byte(secret)) {
			t.Fatal("metadata secret leak")
		}
	}
	got, _, ok, e := s.Managed(exit.ID)
	if e != nil || !ok || got.Identity.UUID != m.Identity.UUID || got.Proxy.Password != password {
		t.Fatal("material not recovered")
	}
	before, e := got.Render()
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	// Raw DB bytes don't contain the client's/recoverable proxy private material.
	raw, _ := os.ReadFile(path)
	for _, secret := range []string{password, m.Identity.UUID, m.Identity.RealityPrivateKey} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("recoverable secret stored plaintext")
		}
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	got, _, ok, e = s.Managed(exit.ID)
	after, renderErr := got.Render()
	if e != nil || !ok || renderErr != nil || before.URI != after.URI || !bytes.Equal(before.ServerJSON, after.ServerJSON) {
		t.Fatal("restart replaced client identity")
	}
	if _, e = s.RotateCredentials(exit.ID); e == nil {
		t.Fatal("rotation desynced managed binding")
	}
	if _, _, e = s.CreateExit(domain.ExitInput{Name: "collision", Country: "JP", Port: m.Request.Port}); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("exit overlaps managed entrance")
	}
	_, wrongHash, _ := s.ExitForRuntime(exit.ID)
	wrong := m
	wrong.Proxy.Password = "wrong"
	if e = s.SaveManaged(exit.ID, wrong, wrongHash); e == nil {
		t.Fatal("wrong auth saved")
	}
	s.Close()
	if e = os.Rename(path+".key", path+".saved-key"); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(path); !errors.Is(e, secretstore.ErrKey) {
		t.Fatal("lost material key recreated")
	}
}
func TestManagedCiphertextCannotMoveAcrossExitsAndDeleteCascades(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	one, p, _ := s.CreateExit(domain.ExitInput{Name: "one", Country: "JP"})
	two, p2, _ := s.CreateExit(domain.ExitInput{Name: "two", Country: "JP"})
	_, hash, _ := s.ExitForRuntime(one.ID)
	_, hash2, _ := s.ExitForRuntime(two.ID)
	if e = s.SaveManaged(one.ID, managedFixture(one, p, 25002), hash); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveManaged(two.ID, managedFixture(two, p2, 25003), hash2); e != nil {
		t.Fatal(e)
	}
	s.db.Exec("UPDATE managed_ingress SET sealed=(SELECT sealed FROM managed_ingress WHERE exit_id=?) WHERE exit_id=?", one.ID, two.ID)
	if _, _, _, e = s.Managed(two.ID); !errors.Is(e, secretstore.ErrCiphertext) {
		t.Fatal("recipient binding bypass")
	}
	if e = s.DeleteExit(one.ID); e != nil {
		t.Fatal(e)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM managed_ingress WHERE exit_id=?", one.ID).Scan(&count)
	if count != 0 {
		t.Fatal("orphan managed secrets")
	}
}
