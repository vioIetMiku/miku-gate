package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedUpstreamPersistenceReuseAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	e, _, err := s.CreateExit(domain.ExitInput{Name: "upstream", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path + ".key"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unused key created")
	}
	in := domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443, Username: "user", Password: "a-unique-upstream-secret"}
	if err = s.SaveUpstream(e.ID, in); err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err = s.db.QueryRow("SELECT sealed FROM upstreams WHERE exit_id=?", e.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(in.Password)) {
		t.Fatal("plaintext secret stored")
	}
	c, updated, ok, err := s.Upstream(e.ID)
	if err != nil || !ok || c.Password != in.Password {
		t.Fatal("decryption", err)
	}
	view, _ := json.Marshal(c.View(updated))
	if bytes.Contains(view, []byte(in.Password)) {
		t.Fatal("secret leaked in view")
	}
	reuse := in
	reuse.Password = ""
	reuse.ReusePassword = true
	reuse.DNS = "google"
	if err = s.SaveUpstream(e.ID, reuse); err != nil {
		t.Fatal(err)
	}
	reuse.Host = "other.example"
	if err = s.SaveUpstream(e.ID, reuse); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("old secret sent to new recipient", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c, _, ok, err = s.Upstream(e.ID)
	if err != nil || !ok || c.Password != in.Password || c.DNS != "google" || c.Host != in.Host {
		t.Fatal("wrong restored config", err)
	}
	if err = s.DeleteExit(e.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM upstreams").Scan(&count)
	if count != 0 {
		t.Fatal("orphaned encrypted config")
	}
}
func TestProtectedUpstreamMissingKeyAndWrongRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := s.CreateExit(domain.ExitInput{Name: "one", Country: "JP"})
	b, _, _ := s.CreateExit(domain.ExitInput{Name: "two", Country: "JP"})
	in := domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443}
	if err = s.SaveUpstream(a.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO upstreams SELECT ?,kind,sealed,updated_at FROM upstreams WHERE exit_id=?", b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.Upstream(b.ID); !errors.Is(err, secretstore.ErrCiphertext) {
		t.Fatal("ciphertext moved across owners", err)
	}
	s.Close()
	if err = os.Remove(path + ".key"); err != nil {
		t.Fatal(err)
	}
	if next, err := Open(path); !errors.Is(err, secretstore.ErrKey) {
		if next != nil {
			next.Close()
		}
		t.Fatal("missing key silently recreated", err)
	}
}
