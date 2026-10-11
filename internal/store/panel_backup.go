package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mikugate/internal/security"
	"strings"
)

const panelPrefix = "panel-backup-v1:"
const panelMax = 3 << 20

// Chunked AEAD keeps the existing vault bound, binds chunk count/order and
// instance + backup identity. Login password/cookie never belongs in payload.
func (s *Store) SavePanelBackup(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > panelMax || !json.Valid(raw) {
		return "", errors.New("面板私有快照超预算或无效")
	}
	var count int
	if s.db.QueryRow("SELECT count(*) FROM metadata WHERE key LIKE 'panel-backup-v1:%'").Scan(&count) != nil || count >= 8 {
		return "", errors.New("最多8份面板快照；请保留/整理旧私有备份后再更改")
	}
	id, e := security.Token(16)
	if e != nil {
		return "", e
	}
	instance, e := s.InstanceID()
	if e != nil {
		return "", e
	}
	vault, e := s.secretVault()
	if e != nil {
		return "", e
	}
	parts := [][]byte{}
	total := (len(raw) + (128 << 10) - 1) / (128 << 10)
	for offset := 0; offset < len(raw); offset += 128 << 10 {
		end := offset + (128 << 10)
		if end > len(raw) {
			end = len(raw)
		}
		sealed, e := vault.Seal(fmt.Sprintf("miku-panel-v1:%s:%s:%d/%d", instance, id, len(parts), total), raw[offset:end])
		if e != nil {
			return "", e
		}
		parts = append(parts, sealed)
	}
	encoded, e := json.Marshal(parts)
	if e != nil {
		return "", e
	}
	_, e = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?)", panelPrefix+id, base64.StdEncoding.EncodeToString(encoded))
	return id, e
}
func (s *Store) PanelBackup(id string) ([]byte, error) {
	if strings.ContainsAny(id, ":/\\") || len(id) != 22 {
		return nil, errors.New("面板快照身份无效")
	}
	var encoded string
	if e := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", panelPrefix+id).Scan(&encoded); e != nil {
		return nil, e
	}
	if len(encoded) > 6<<20 {
		return nil, errors.New("面板快照超预算")
	}
	raw, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return nil, e
	}
	var parts [][]byte
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 || len(parts) > 24 {
		return nil, errors.New("面板快照分块无效")
	}
	instance, e := s.InstanceID()
	if e != nil {
		return nil, e
	}
	vault, e := s.secretVault()
	if e != nil {
		return nil, e
	}
	plain := []byte{}
	for i, sealed := range parts {
		chunk, e := vault.Open(fmt.Sprintf("miku-panel-v1:%s:%s:%d/%d", instance, id, i, len(parts)), sealed)
		if e != nil {
			clear(plain)
			return nil, e
		}
		plain = append(plain, chunk...)
		clear(chunk)
		if len(plain) > panelMax {
			clear(plain)
			return nil, errors.New("面板快照超预算")
		}
	}
	if !json.Valid(plain) {
		clear(plain)
		return nil, errors.New("面板快照无法验证")
	}
	return plain, nil
}
func (s *Store) PanelBackupIDs() ([]string, error) {
	rows, e := s.db.Query("SELECT key FROM metadata WHERE key LIKE 'panel-backup-v1:%' ORDER BY key LIMIT 9")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			return nil, e
		}
		ids = append(ids, strings.TrimPrefix(key, panelPrefix))
	}
	if len(ids) > 8 {
		return nil, errors.New("面板快照数量异常")
	}
	return ids, rows.Err()
}

// ForgetPanelBackup only removes this private checkpoint; not applied routes.
func (s *Store) ForgetPanelBackup(id string) error {
	pending, e := s.PanelPending()
	if e != nil {
		return e
	}
	if pending == id {
		return errors.New("该快照属于未完成面板事务，先恢复，不允许删除")
	}
	private, e := s.PanelBackup(id)
	clear(private)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("DELETE FROM metadata WHERE key=?", panelPrefix+id)
	return e
}

func (s *Store) SetPanelPending(id string) error {
	private, e := s.PanelBackup(id)
	clear(private)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO metadata(key,value) VALUES('panel-pending-v1',?)", id)
	return e
}
func (s *Store) ClearPanelPending(id string) error {
	_, e := s.db.Exec("DELETE FROM metadata WHERE key='panel-pending-v1' AND value=?", id)
	return e
}
func (s *Store) PanelPending() (string, error) {
	var id string
	e := s.db.QueryRow("SELECT value FROM metadata WHERE key='panel-pending-v1'").Scan(&id)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return id, e
}
