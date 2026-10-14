package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

type ManagedView struct {
	Configured          bool                   `json:"configured"`
	ExitID              string                 `json:"exit_id"`
	Request             ingress.ManagedRequest `json:"request"`
	UpdatedAt           string                 `json:"updated_at"`
	Applied             bool                   `json:"applied"`
	RealityCheckedAt    *time.Time             `json:"reality_checked_at,omitempty"`
	RealityNotAfter     *time.Time             `json:"reality_not_after,omitempty"`
	RealityNeedsRefresh bool                   `json:"reality_needs_refresh"`
	CoreRollback        *ingress.CoreRollback  `json:"core_rollback,omitempty"`
}

func (s *Store) managedLabel(id string) (string, error) {
	instance, e := s.InstanceID()
	return "miku-managed-v1:" + instance + ":" + id, e
}
func (s *Store) Managed(id string) (ingress.ManagedMaterial, string, bool, error) {
	m, updated, ok, _, err := s.ManagedSnapshot(id)
	return m, updated, ok, err
}

// The revision is a hash of authenticated encrypted bytes from this same read.
// It reveals no config and changes on any saved material update.
func (s *Store) ManagedSnapshot(id string) (ingress.ManagedMaterial, string, bool, string, error) {
	var m ingress.ManagedMaterial
	var core, updated string
	var port int
	var sealed []byte
	e := s.db.QueryRow("SELECT core,port,sealed,updated_at FROM managed_ingress WHERE exit_id=?", id).Scan(&core, &port, &sealed, &updated)
	if errors.Is(e, sql.ErrNoRows) {
		_, _, e = s.ExitForRuntime(id)
		return m, "", false, "", e
	}
	if e != nil {
		return m, "", false, "", e
	}
	vault, e := s.secretVault()
	if e != nil {
		return m, "", false, "", e
	}
	label, e := s.managedLabel(id)
	if e != nil {
		return m, "", false, "", e
	}
	plain, e := vault.Open(label, sealed)
	if e != nil {
		return m, "", false, "", e
	}
	defer clear(plain)
	if json.Unmarshal(plain, &m) != nil || m.Request.Core != core || m.Request.Port != port || m.Proxy.Validate() != nil || m.Identity.Validate(m.Request.Template == "vless-reality-vision") != nil || m.CoreRollback.Validate(core) != nil || m.ValidateDevices() != nil {
		return ingress.ManagedMaterial{}, "", false, "", secretstore.ErrCiphertext
	}
	return m, updated, true, sealedRevision(sealed), nil
}
func sealedRevision(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (s *Store) ManagedView(id string) (ManagedView, error) {
	m, updated, ok, e := s.Managed(id)
	if e != nil {
		return ManagedView{}, e
	}
	view := ManagedView{Configured: ok, ExitID: id, Request: m.Request, UpdatedAt: updated, CoreRollback: m.CoreRollback}
	if ok && m.Request.Template == "vless-reality-vision" {
		view.RealityNeedsRefresh = true
		if m.Target != nil {
			view.RealityCheckedAt = &m.Target.CheckedAt
			view.RealityNotAfter = &m.Target.NotAfter
			view.RealityNeedsRefresh = m.Target.NeedsRefresh(time.Now()) || m.Target.Hostname != m.Request.RealityHostname
		}
	}
	return view, nil
}
func (s *Store) SaveManaged(id string, material ingress.ManagedMaterial, expectedPasswordHash string) error {
	return s.saveManaged(context.Background(), id, material, expectedPasswordHash, "", "")
}
func (s *Store) ReplaceManaged(ctx context.Context, id string, material ingress.ManagedMaterial, hash, revision string) error {
	if raw, e := hex.DecodeString(revision); e != nil || len(raw) != 32 || hex.EncodeToString(raw) != revision {
		return domain.ErrConflict
	}
	return s.saveManaged(ctx, id, material, hash, revision, "core")
}
func (s *Store) ReplaceManagedClients(ctx context.Context, id string, material ingress.ManagedMaterial, hash, revision string) error {
	if raw, e := hex.DecodeString(revision); e != nil || len(raw) != 32 || hex.EncodeToString(raw) != revision {
		return domain.ErrConflict
	}
	return s.saveManaged(ctx, id, material, hash, revision, "clients")
}
func (s *Store) ReplaceManagedTarget(ctx context.Context, id string, material ingress.ManagedMaterial, hash, revision string) error {
	before, _, ok, currentRevision, err := s.ManagedSnapshot(id)
	if err != nil {
		return err
	}
	unchanged := material
	unchanged.Target = before.Target
	if !ok || revision == "" || currentRevision != revision || !reflect.DeepEqual(before, unchanged) {
		return domain.ErrConflict
	}
	return s.saveManaged(ctx, id, material, hash, revision, "target")
}
func (s *Store) saveManaged(ctx context.Context, id string, material ingress.ManagedMaterial, expectedPasswordHash, revision, change string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	exit, hash, e := s.ExitForRuntime(id)
	if e != nil {
		return e
	}
	if hash != expectedPasswordHash || !security.VerifyPassword(hash, material.Proxy.Password) {
		return domain.ErrConflict
	}
	if change == "clients" {
		original, _, ok, currentRevision, err := s.ManagedSnapshot(id)
		if err != nil {
			return err
		}
		if !ok || currentRevision != revision || material.ValidateDevices() != nil || !validClientChange(original, material) {
			return domain.ErrConflict
		}
		// Revocation must work with expired certificates/REALITY proofs. Only
		// client authorization may change; transport validation occurs on start.
	} else if e = material.ValidateBinding(exit.Port, exit.Username); e != nil {
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
	tx, e := s.db.BeginTx(ctx, nil)
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
	if revision != "" {
		var currentSealed []byte
		if e = tx.QueryRow("SELECT sealed FROM managed_ingress WHERE exit_id=?", id).Scan(&currentSealed); e != nil || sealedRevision(currentSealed) != revision {
			return domain.ErrConflict
		}
	}
	var conflict int
	if e = tx.QueryRow("SELECT (SELECT count(*) FROM exits WHERE port=?)+(SELECT count(*) FROM entrances WHERE port=?)", material.Request.Port, material.Request.Port).Scan(&conflict); e != nil {
		return e
	}
	if conflict > 0 {
		return domain.ErrConflict
	}
	_, e = tx.Exec("INSERT INTO managed_ingress(exit_id,core,port,sealed,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(exit_id) DO UPDATE SET core=excluded.core,port=excluded.port,sealed=excluded.sealed,updated_at=excluded.updated_at", id, material.Request.Core, material.Request.Port, sealed, time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		return domain.ErrConflict
	}
	if revision != "" && change != "target" {
		// Confirmed core/client changes must not be followed by an old
		// resume permission reopening a listener. Commit this intent atomically.
		_, e = tx.Exec(`INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=json_set(metadata.value,'$.resume_ingress',json('false'))`, "recovery-v1:"+id, `{"enabled":false,"resume_ingress":false,"allow_legacy":false,"node_ids":[],"confirm":false}`)
		if e != nil {
			return e
		}
		kind, message := "ingress.core_changed", "已校验并切换受管核心选择；保留身份/素材，入口恢复权限取消，未启动"
		if change == "clients" {
			kind, message = "ingress.clients_changed", "已更新独立设备授权；其他身份保持，入口恢复权限取消，未启动"
		}
		if e = event(tx, "info", kind, message, id); e != nil {
			return e
		}
	}
	if e = event(tx, "info", "ingress.configured", "已加密保存独立受管入口；未启动，不接管已有面板", id); e != nil {
		return e
	}
	return tx.Commit()
}
func validClientChange(before, after ingress.ManagedMaterial) bool {
	if before.LegacyDisabled && !after.LegacyDisabled {
		return false
	}
	unchanged := after
	unchanged.Devices, unchanged.LegacyDisabled = before.Devices, before.LegacyDisabled
	if !reflect.DeepEqual(before, unchanged) {
		return false
	}
	for _, old := range before.Devices {
		found := false
		for _, next := range after.Devices {
			if next.ID != old.ID {
				continue
			}
			found = true
			copy := next
			copy.RevokedAt = old.RevokedAt
			if !reflect.DeepEqual(old, copy) || (old.RevokedAt != nil && !reflect.DeepEqual(old.RevokedAt, next.RevokedAt)) {
				return false
			}
		}
		if !found && old.RevokedAt == nil {
			return false
		}
	}
	return true
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
