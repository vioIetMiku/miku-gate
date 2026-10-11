package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelSnapshotEncryptedChunksIdentityMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte(`{"template":"` + strings.Repeat("private-panel-route-secret", 9000) + `"}`)
	id, e := s.SavePanelBackup(raw)
	if e != nil {
		t.Fatal(e)
	}
	recovered, e := s.PanelBackup(id)
	if e != nil || !bytes.Equal(recovered, raw) {
		t.Fatal("chunked snapshot failed", e)
	}
	checked, e := InspectMaintenance(path)
	if e != nil || checked.EncryptedRecordCount != 1 || checked.Schema != 7 {
		t.Fatal(checked, e)
	}
	var sealed string
	s.db.QueryRow("SELECT value FROM metadata WHERE key=?", panelPrefix+id).Scan(&sealed)
	if strings.Contains(sealed, "private-panel-route-secret") {
		t.Fatal("snapshot plaintext")
	}
	s.Close()
	os.Remove(path + ".key")
	if _, e = Open(path); e == nil {
		t.Fatal("missing panel-only key regenerated")
	}
	if _, e = os.Stat(path + ".key"); !os.IsNotExist(e) {
		t.Fatal("missing key automatically created")
	}
}
func TestPanelSnapshotRecipientAndCapacity(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, e := s.SavePanelBackup([]byte(`{"private":"first"}`))
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec("INSERT INTO metadata(key,value) SELECT ?,value FROM metadata WHERE key=?", panelPrefix+strings.Repeat("b", 22), panelPrefix+id)
	if _, e = s.PanelBackup(strings.Repeat("b", 22)); e == nil {
		t.Fatal("recipient ciphertext copied")
	}
	for i := 0; i < 6; i++ {
		if _, e = s.SavePanelBackup([]byte(`{"fixture":true}`)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.SavePanelBackup([]byte(`{"fixture":true}`)); e == nil {
		t.Fatal("unbounded backups")
	}
}

func TestPendingPanelRecoveryCannotLoseItsSnapshot(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, e := s.SavePanelBackup([]byte(`{"private":"checkpoint"}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetPanelPending(id); e != nil {
		t.Fatal(e)
	}
	if e = s.ForgetPanelBackup(id); e == nil {
		t.Fatal("pending recovery checkpoint deleted")
	}
	if _, e = s.SavePanelBackup([]byte(`{"other":true}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.ClearPanelPending(id); e != nil {
		t.Fatal(e)
	}
	if e = s.ForgetPanelBackup(id); e != nil {
		t.Fatal(e)
	}
}
