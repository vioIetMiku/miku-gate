package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
)

func (s *Store) secretVault() (*secretstore.Vault, error) {
	s.vaultMu.Lock()
	defer s.vaultMu.Unlock()
	if s.vault != nil {
		return s.vault, nil
	}
	var count int
	var entranceTable int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='entrances'").Scan(&entranceTable); err != nil {
		return nil, err
	}
	query := "SELECT (SELECT count(*) FROM upstreams)+(SELECT count(*) FROM vpn_configs)+(SELECT count(*) FROM managed_ingress)+(SELECT count(*) FROM entrances)+(SELECT count(*) FROM entrance_exits)+(SELECT count(*) FROM metadata WHERE key LIKE 'panel-backup-v1:%' OR key LIKE 'subscription-v1:%' OR key LIKE 'fleet-v1:%' OR key LIKE 'proxy-v1:%')"
	if entranceTable == 0 {
		query = strings.ReplaceAll(query, "(SELECT count(*) FROM entrances)+(SELECT count(*) FROM entrance_exits)+", "")
	}
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return nil, err
	}
	v, err := secretstore.Open(s.path+".key", count == 0)
	if err != nil {
		return nil, err
	}
	s.vault = v
	return v, nil
}
func (s *Store) ExitForRuntime(id string) (domain.Exit, string, error) {
	var e domain.Exit
	var hash, created string
	err := s.db.QueryRow("SELECT id,name,country,policy,port,listen,username,password_hash,status,reason,created_at FROM exits WHERE id=?", id).Scan(&e.ID, &e.Name, &e.Country, &e.Policy, &e.Port, &e.Listen, &e.Username, &hash, &e.Status, &e.Reason, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return e, "", domain.ErrNotFound
	}
	if err != nil {
		return e, "", err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return e, hash, nil
}

type protectedUpstream struct {
	Config domain.UpstreamConfig `json:"config"`
	Secret string                `json:"secret"`
}

func (s *Store) upstreamLabel(id string) (string, error) {
	instance, err := s.InstanceID()
	return "miku-upstream-v1:" + instance + ":" + id, err
}
func (s *Store) Upstream(id string) (domain.UpstreamConfig, string, bool, error) {
	var config domain.UpstreamConfig
	var kind, updated string
	var sealed []byte
	err := s.db.QueryRow("SELECT kind,sealed,updated_at FROM upstreams WHERE exit_id=?", id).Scan(&kind, &sealed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		_, _, err = s.ExitForRuntime(id)
		return config, "", false, err
	}
	if err != nil {
		return config, "", false, err
	}
	v, err := s.secretVault()
	if err != nil {
		return config, "", false, err
	}
	label, err := s.upstreamLabel(id)
	if err != nil {
		return config, "", false, err
	}
	plain, err := v.Open(label, sealed)
	if err != nil {
		return config, "", false, err
	}
	defer clear(plain)
	var stored protectedUpstream
	if err = json.Unmarshal(plain, &stored); err != nil {
		return config, "", false, secretstore.ErrCiphertext
	}
	config = stored.Config
	config.Password = stored.Secret
	if config.Kind != kind || config.Validate() != nil {
		return domain.UpstreamConfig{}, "", false, secretstore.ErrCiphertext
	}
	return config, updated, true, nil
}
func (s *Store) SaveUpstream(id string, in domain.UpstreamInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if _, _, err := s.ExitForRuntime(id); err != nil {
		return err
	}
	var vpnCount int
	if err := s.db.QueryRow("SELECT count(*) FROM vpn_configs WHERE exit_id=?", id).Scan(&vpnCount); err != nil {
		return err
	}
	if vpnCount > 0 && !in.ReplaceVPN {
		return fmt.Errorf("%w: 请明确确认替换此出口现有OpenVPN绑定", domain.ErrConflict)
	}
	config := in.Config()
	if in.ReusePassword {
		old, _, exists, err := s.Upstream(id)
		if err != nil {
			return err
		}
		if !exists || old.Password == "" || !config.SameCredentialTarget(old) {
			return fmt.Errorf("%w: 仅能对相同上游地址、协议、TLS身份及用户名复用密码；更换目标需明确输入新凭据", domain.ErrConflict)
		}
		config.Password = old.Password
	}
	if err := config.Validate(); err != nil {
		return err
	}
	plain, err := json.Marshal(protectedUpstream{Config: config, Secret: config.Password})
	if err != nil {
		return err
	}
	defer clear(plain)
	v, err := s.secretVault()
	if err != nil {
		return err
	}
	label, err := s.upstreamLabel(id)
	if err != nil {
		return err
	}
	sealed, err := v.Seal(label, plain)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM vpn_configs WHERE exit_id=?", id); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO upstreams(exit_id,kind,sealed,updated_at) VALUES(?,?,?,?) ON CONFLICT(exit_id) DO UPDATE SET kind=excluded.kind,sealed=excluded.sealed,updated_at=excluded.updated_at", id, config.Kind, sealed, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM metadata WHERE key=?", "standby-v1:"+id); err != nil {
		return err
	}
	if err = event(tx, "info", "exit.upstream_configured", "已保存"+config.Kind+"代理上游；凭据加密保存在本机", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteUpstream(id string) error {
	if _, _, err := s.ExitForRuntime(id); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM upstreams WHERE exit_id=?", id); err != nil {
		return err
	}
	if err = event(tx, "info", "exit.upstream_removed", "已移除代理上游配置", id); err != nil {
		return err
	}
	return tx.Commit()
}
