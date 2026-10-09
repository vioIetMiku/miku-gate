package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectInstanceDoesNotMigrateOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miku db.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	expected, _ := s.InstanceID()
	s.db.Exec("PRAGMA user_version=2")
	actual, err := InspectInstance(path)
	if err != nil || actual != expected {
		t.Fatal(actual, err)
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("readonly inspection migrated data", version, err)
	}
	missing := filepath.Join(t.TempDir(), "absent", "db")
	if _, err = InspectInstance(missing); err == nil {
		t.Fatal("missing inspection succeeded")
	}
	if _, err = os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("readonly inspection created data")
	}
}
