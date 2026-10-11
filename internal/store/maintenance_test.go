package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mikugate/internal/domain"
)

func TestMaintenanceChecksProtectedPairWithoutMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	exit, _, err := s.CreateExit(domain.ExitInput{Name: "maintenance", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveUpstream(exit.ID, domain.UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443, Username: "fixture", Password: "private-pair-fixture"}); err != nil {
		t.Fatal(err)
	}
	instance, _ := s.InstanceID()
	s.Close()
	key, err := os.ReadFile(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := InspectMaintenance(path)
	if err != nil || inspected.Instance != instance || inspected.Schema != 6 || inspected.EncryptedRecordCount != 1 || !inspected.ProtectedRecordsVerified {
		t.Fatal(inspected, err)
	}
	after, _ := os.ReadFile(path + ".key")
	if !bytes.Equal(key, after) {
		t.Fatal("maintenance changed key")
	}
	if err = os.WriteFile(path+".key", bytes.Repeat([]byte{1}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = InspectMaintenance(path); err == nil {
		t.Fatal("mismatched paired key accepted")
	}
}

func TestMaintenanceDoesNotCreateMissingKeyOrMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = InspectMaintenance(path); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path + ".key"); !os.IsNotExist(err) {
		t.Fatal("readonly maintenance created unused key")
	}
	s.db.Exec("PRAGMA user_version=5")
	if _, err = InspectMaintenance(path); err == nil {
		t.Fatal("unsupported schema accepted")
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 5 {
		t.Fatal("maintenance migrated schema")
	}
	s.Close()
	missing := filepath.Join(t.TempDir(), "absent", "db")
	if _, err = InspectMaintenance(missing); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err = os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("readonly maintenance created data path")
	}
}
