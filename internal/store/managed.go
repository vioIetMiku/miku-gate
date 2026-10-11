package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

type ManagedView struct {
	Configured bool                   `json:"configured"`
	ExitID     string                 `json:"exit_id"`
	Request    ingress.ManagedRequest `json:"request"`
	UpdatedAt  string                 `json:"updated_at"`
	Applied    bool                   `json:"applied"`
}

func (s *Store) managedLabel(id string) (string, error) {
	instance, e := s.InstanceID()
	return "miku-managed-v1:" + instance + ":" + id, e
}
func (s *Store) Managed(id string) (ingress.ManagedMaterial, string, bool, error) {
	var m ingress.ManagedMaterial
	var core, updated string
	var port int
	var sealed []byte
	e := s.db.QueryRow("SELECT core,port,sealed,updated_at FROM managed_ingress WHERE exit_id=?", id).Scan(&core, &port, &sealed, &updated)
	if errors.Is(e, sql.ErrNoRows) {
		_, _, e = s.ExitForRuntime(id)
		return m, "", false, e
	}
	if e != nil {
		return m, "", false, e
	}
	vault, e := s.secretVault()
	if e != nil {
		return m, "", false, e
	}
	label, e := s.managedLabel(id)
	if e != nil {
		return m, "", false, e
	}
	plain, e := vault.Open(label, sealed)
	if e != nil {
		return m, "", false, e
	}
	defer clear(plain)
	if json.Unmarshal(plain, &m) != nil || m.Request.Core != core || m.Request.Port != port || m.Proxy.Validate() != nil || m.Identity.Validate(m.Request.Template == "vless-reality-vision") != nil {
		return ingress.ManagedMaterial{}, "", false, secretstore.ErrCiphertext
	}
	return m, updated, true, nil
}
func (s *Store) ManagedView(id string) (ManagedView, error) {
	m, updated, ok, e := s.Managed(id)
	if e != nil {
		return ManagedView{}, e
	}
	return ManagedView{Configured: ok, ExitID: id, Request: m.Request, UpdatedAt: updated}, nil
}
func (s *Store) SaveManaged(id string, material ingress.ManagedMaterial, expectedPasswordHash string) error {
	exit, hash, e := s.ExitForRuntime(id)
	if e != nil {
		return e
	}
	if hash != expectedPasswordHash || !security.VerifyPassword(hash, material.Proxy.Password) {
		return domain.ErrConflict
	}
	if e = material.ValidateBinding(exit.Port, exit.Username); e != nil {
		return e
	}
	if material.Request.Port < 1024 {
		return errors.New("当前普通用户受管入口需1024以上端口；低端口由后续安装服务明确授权")
	}
	raw, e := json.Marshal(material)
	if e != nil {
		return e
	}
	defer clear(raw)
	vault, e := s.secretVault()
	if e != nil {
		return e
	}
	label, e := s.managedLabel(id)
	if e != nil {
		return e
	}
	sealed, e := vault.Seal(label, raw)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var current string
	if e = tx.QueryRow("SELECT password_hash FROM exits WHERE id=?", id).Scan(&current); e != nil {
		return e
	}
	if current != hash {
		return domain.ErrConflict
	}
	var conflict int
	if e = tx.QueryRow("SELECT count(*) FROM exits WHERE port=?", material.Request.Port).Scan(&conflict); e != nil {
		return e
	}
	if conflict > 0 {
		return domain.ErrConflict
	}
	_, e = tx.Exec("INSERT INTO managed_ingress(exit_id,core,port,sealed,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(exit_id) DO UPDATE SET core=excluded.core,port=excluded.port,sealed=excluded.sealed,updated_at=excluded.updated_at", id, material.Request.Core, material.Request.Port, sealed, time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		return domain.ErrConflict
	}
	if e = event(tx, "info", "ingress.configured", "已加密保存独立受管入口；未启动，不接管已有面板", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) DeleteManaged(id string) error {
	if _, _, e := s.ExitForRuntime(id); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM managed_ingress WHERE exit_id=?", id); e != nil {
		return e
	}
	if e = event(tx, "info", "ingress.removed", "已删除受管入口配置", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) HasManaged(id string) (bool, error) {
	var n int
	e := s.db.QueryRow("SELECT count(*) FROM managed_ingress WHERE exit_id=?", id).Scan(&n)
	return n > 0, e
}
