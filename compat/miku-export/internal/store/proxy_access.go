package store

import (
	"database/sql"
	"encoding/base64"
	"errors"

	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

func (s *Store) sealProxyPassword(id, password string) (string, error) {
	vault, err := s.secretVault()
	if err != nil {
		return "", err
	}
	instance, err := s.InstanceID()
	if err != nil {
		return "", err
	}
	plain := []byte(password)
	defer clear(plain)
	sealed, err := vault.Seal("miku-proxy-v1:"+instance+":"+id, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// ProxyPassword is privileged material: callers must explicitly authorize its
// display. It never fabricates a password for old hash-only exits.
func (s *Store) ProxyPassword(id string) (string, error) {
	_, hash, err := s.ExitForRuntime(id)
	if err != nil {
		return "", err
	}
	var encoded string
	err = s.db.QueryRow("SELECT value FROM metadata WHERE key=?", "proxy-v1:"+id).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		material, _, ok, e := s.Managed(id)
		if e != nil {
			return "", e
		}
		if ok && security.VerifyPassword(hash, material.Proxy.Password) {
			return material.Proxy.Password, nil
		}
		return "", errors.New("旧出口只保存密码摘要；请先停止出口并轮换凭据后再复制")
	}
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", secretstore.ErrCiphertext
	}
	vault, err := s.secretVault()
	if err != nil {
		return "", err
	}
	instance, err := s.InstanceID()
	if err != nil {
		return "", err
	}
	plain, err := vault.Open("miku-proxy-v1:"+instance+":"+id, sealed)
	if err != nil {
		return "", err
	}
	defer clear(plain)
	if !security.VerifyPassword(hash, string(plain)) {
		return "", secretstore.ErrCiphertext
	}
	return string(plain), nil
}

func (s *Store) SetProxyPublic(id string, public bool) error {
	listen := "127.0.0.1"
	if public {
		listen = "0.0.0.0"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE exits SET listen=? WHERE id=?", listen, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	if err = event(tx, "info", "exit.access_changed", "已修改认证代理监听范围："+listen+"；未启动", id); err != nil {
		return err
	}
	return tx.Commit()
}
