package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/security"
)

func TestPersistenceAndState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miku.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	hash, _ := security.HashPassword("test")
	if err = s.InitAccount("admin", hash); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.InitAccount("other", hash), domain.ErrConflict) {
		t.Fatal("overwrote account")
	}
	e, password, err := s.CreateExit(domain.ExitInput{Name: "日本开发", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 20000 || password == "" || e.Status != "stopped" {
		t.Fatal("wrong defaults")
	}
	if _, _, err = s.CreateExit(domain.ExitInput{Name: "冲突", Country: "US", Port: 20000}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("port conflict ignored")
	}
	if err = s.SetStatus(e.ID, "active", "pretend"); err == nil {
		t.Fatal("fake active status allowed")
	}
	if err = s.SetStatus(e.ID, "blocked", "no executor"); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateExit(e.ID, domain.ExitInput{Name: "新名字", Country: "KR"}); err != nil {
		t.Fatal(err)
	}
	exits, _ := s.ListExits()
	if len(exits) != 1 || exits[0].Port != 20000 || exits[0].Status != "blocked" {
		t.Fatal("bad update")
	}
	if err = s.CreateSession("admin", "raw-secret", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session("raw-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session("bad"); err == nil {
		t.Fatal("bad session accepted")
	}
	if err = s.CreateSession("admin", "expired", "csrf", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session("expired"); err == nil {
		t.Fatal("expired session")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	exits, _ = s.ListExits()
	if len(exits) != 1 || exits[0].Name != "新名字" {
		t.Fatal("state not durable")
	}
	if err = s.DeleteExit(e.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.DeleteExit(e.ID), domain.ErrNotFound) {
		t.Fatal("missing deletion")
	}
	events, _ := s.Events()
	if len(events) < 5 {
		t.Fatal("missing audit events")
	}
}
func TestNodeCacheAndEventBound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := domain.Node{ID: "one", Hostname: "node", IP: "8.8.8.8", Country: "JP", Source: "official", FetchedAt: time.Now(), Config: "private-config"}
	if err = s.ReplaceNodes(context.Background(), []domain.Node{n}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceNodes(context.Background(), nil); err == nil {
		t.Fatal("empty overwrite allowed")
	}
	nodes, _ := s.ListNodes()
	if len(nodes) != 1 || nodes[0].Config != "" || nodes[0].Type != "unknown" {
		t.Fatal("cache exposed private config or fabricated type")
	}
	for i := 0; i < 505; i++ {
		if err = s.AddEvent("info", "test", "safe", ""); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	if count != 500 {
		t.Fatalf("unbounded audit log: %d", count)
	}
}

func TestRejectFutureDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=9"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("newer schema opened by older executable")
	}
}

func TestInstanceIdentityMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := s.CreateExit(domain.ExitInput{Name: "迁移出口", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the v1 schema: the only v2 addition is metadata + version 2.
	if _, err = s.db.Exec("DROP TABLE metadata; DELETE FROM schema_version WHERE version>=2; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.InstanceID()
	if err != nil || len(first) != 22 {
		t.Fatal("missing stable instance identity", err)
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("migration not committed", err)
	}
	exits, err := s.ListExits()
	if err != nil || len(exits) != 1 || exits[0].ID != e.ID || exits[0].Port != e.Port {
		t.Fatal("migration changed exit configuration", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.InstanceID()
	if err != nil || again != first {
		t.Fatal("instance ID changed after restart", err)
	}
	other, err := Open(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	different, _ := other.InstanceID()
	if different == first {
		t.Fatal("separate installations share an identity")
	}
}
func TestCorruptInstanceIdentityIsNotSilentlyReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-identity.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE metadata SET value='../not-safe' WHERE key='instance_id'"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if opened, err := Open(path); err == nil {
		opened.Close()
		t.Fatal("invalid resource identity accepted or replaced")
	}
}

func TestMissingV2InstanceIdentityIsNotRegenerated(t *testing.T) {
	for _, statement := range []string{"DELETE FROM metadata WHERE key='instance_id'", "DROP TABLE metadata"} {
		t.Run(statement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing-identity.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if reopened, err := Open(path); err == nil {
				reopened.Close()
				t.Fatal("silently replaced missing v2 resource identity")
			}
		})
	}
}
