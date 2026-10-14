package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"mikugate/internal/domain"
	"mikugate/internal/security"
)

func (s *Store) NodePreferences() ([]domain.NodePreference, error) {
	rows, err := s.db.Query("SELECT node_id,value FROM node_preferences ORDER BY node_id LIMIT 2000")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.NodePreference{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var p domain.NodePreference
		if json.Unmarshal([]byte(raw), &p) != nil || p.NodeID != id {
			return nil, errors.New("节点偏好记录无效")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func cleanText(value string, maximum int) bool {
	if !utf8.ValidString(value) || len([]rune(value)) > maximum {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func (s *Store) SaveNodePreference(p domain.NodePreference) error {
	if _, err := s.NodeForRuntime(p.NodeID); err != nil {
		return err
	}
	if !cleanText(p.Note, 256) || len(p.Tags) > 8 {
		return errors.New("备注最多256字符，标签最多8个")
	}
	seen := map[string]bool{}
	for i, t := range p.Tags {
		t = strings.TrimSpace(t)
		if t == "" || !cleanText(t, 24) || seen[t] {
			return errors.New("标签需为1–24字符且不能重复")
		}
		seen[t] = true
		p.Tags[i] = t
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	p.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO node_preferences(node_id,value) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET value=excluded.value", p.NodeID, string(raw))
	return err
}
func (s *Store) NodeBlocked(id string) (bool, error) {
	var raw string
	err := s.db.QueryRow("SELECT value FROM node_preferences WHERE node_id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	var p domain.NodePreference
	if err == nil {
		err = json.Unmarshal([]byte(raw), &p)
	}
	return p.Blocked, err
}
func (s *Store) DeleteNodePreference(id string) error {
	_, err := s.db.Exec("DELETE FROM node_preferences WHERE node_id=?", id)
	return err
}

func (s *Store) RecordHistory(samples []domain.HistorySample) error {
	if len(samples) > 100 {
		return errors.New("历史采样超出预算")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range samples {
		_, err = tx.Exec("INSERT INTO runtime_history(exit_id,phase,healthy,exit_ip,check_ms,connections,upload,download,run_since,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)", p.ExitID, p.Phase, p.Healthy, p.ExitIP, p.CheckMS, p.Connections, p.Upload, p.Download, p.RunSince, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM runtime_history WHERE id NOT IN (SELECT id FROM runtime_history ORDER BY id DESC LIMIT 20000) OR recorded_at<?", time.Now().Add(-14*24*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) History(id string, limit int) ([]domain.HistorySample, error) {
	if limit < 1 || limit > 1440 {
		limit = 360
	}
	rows, err := s.db.Query("SELECT id,exit_id,phase,healthy,exit_ip,check_ms,connections,upload,download,run_since,recorded_at FROM runtime_history WHERE (?='' OR exit_id=?) ORDER BY id DESC LIMIT ?", id, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.HistorySample{}
	for rows.Next() {
		var p domain.HistorySample
		var stamp string
		if err = rows.Scan(&p.ID, &p.ExitID, &p.Phase, &p.Healthy, &p.ExitIP, &p.CheckMS, &p.Connections, &p.Upload, &p.Download, &p.RunSince, &stamp); err != nil {
			return nil, err
		}
		p.RecordedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) QueryEvents(level, id string, before int64, limit int) ([]domain.Event, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	if level != "" && level != "info" && level != "warning" && level != "error" {
		return nil, errors.New("事件级别无效")
	}
	rows, err := s.db.Query("SELECT id,level,kind,message,exit_id,created_at FROM events WHERE (?='' OR level=?) AND (?='' OR exit_id=?) AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?", level, level, id, id, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		var stamp string
		if err = rows.Scan(&e.ID, &e.Level, &e.Kind, &e.Message, &e.ExitID, &stamp); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) ChangePassword(username, old, new string) error {
	if len(new) < 12 || len(new) > 256 {
		return errors.New("新密码需为12–256字节")
	}
	current, err := s.PasswordHash(username)
	if err != nil || !security.VerifyPassword(current, old) {
		return errors.New("原密码不正确")
	}
	hash, err := security.HashPassword(new)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec("UPDATE accounts SET password_hash=? WHERE username=? AND password_hash=?", hash, username, current)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return domain.ErrConflict
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE username=?", username); err != nil {
		return err
	}
	if err = event(tx, "info", "account.password_changed", "管理员密码已更改，全部旧会话已撤销", ""); err != nil {
		return err
	}
	return tx.Commit()
}

// JSON metadata is restricted to internal fixed prefixes; no caller chooses SQL.
func (s *Store) SaveTask(id string, raw []byte) error {
	if len(id) != 16 || len(raw) > 32768 || !json.Valid(raw) {
		return errors.New("任务记录无效")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "task-v1:"+id, string(raw)); err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM metadata WHERE key LIKE 'task-v1:%' AND key NOT IN (SELECT key FROM metadata WHERE key LIKE 'task-v1:%' ORDER BY json_extract(value,'$.created_at') DESC LIMIT 40)")
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) TaskRecords() ([][]byte, error) {
	rows, err := s.db.Query("SELECT value FROM metadata WHERE key LIKE 'task-v1:%' LIMIT 40")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]byte{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, []byte(raw))
	}
	return out, rows.Err()
}

func (s *Store) CreateAgentToken(in domain.AgentToken) (domain.AgentToken, string, error) {
	if !cleanText(in.Name, 48) || strings.TrimSpace(in.Name) == "" || (in.Role != "viewer" && in.Role != "operator") || len(in.ExitIDs) == 0 || len(in.ExitIDs) > 100 || time.Until(in.ExpiresAt) < time.Hour || time.Until(in.ExpiresAt) > 366*24*time.Hour {
		return in, "", errors.New("令牌需名称、viewer/operator角色、1–100个出口和1小时–366天有效期")
	}
	seen := map[string]bool{}
	for _, id := range in.ExitIDs {
		if seen[id] {
			return in, "", errors.New("出口重复")
		}
		seen[id] = true
		if _, _, err := s.ExitForRuntime(id); err != nil {
			return in, "", err
		}
	}
	id, err := security.Token(12)
	if err != nil {
		return in, "", err
	}
	token, err := security.Token(32)
	if err != nil {
		return in, "", err
	}
	in.ID = id
	in.CreatedAt = time.Now().UTC()
	scope, _ := json.Marshal(in.ExitIDs)
	tx, err := s.db.Begin()
	if err != nil {
		return in, "", err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM agent_tokens").Scan(&n); err != nil {
		return in, "", err
	}
	if n >= 32 {
		return in, "", errors.New("最多32份Agent令牌")
	}
	_, err = tx.Exec("INSERT INTO agent_tokens(id,name,role,scope,token_hash,expires,created_at) VALUES(?,?,?,?,?,?,?)", id, in.Name, in.Role, string(scope), security.Digest(token), in.ExpiresAt.Unix(), in.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return in, "", err
	}
	if err = event(tx, "info", "agent.token_created", "已创建限定出口的Agent令牌", ""); err != nil {
		return in, "", err
	}
	return in, token, tx.Commit()
}
func scanToken(row interface{ Scan(...any) error }) (domain.AgentToken, error) {
	var t domain.AgentToken
	var scope, created string
	var expires int64
	err := row.Scan(&t.ID, &t.Name, &t.Role, &scope, &expires, &created)
	if err == nil {
		err = json.Unmarshal([]byte(scope), &t.ExitIDs)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.ExpiresAt = time.Unix(expires, 0).UTC()
	return t, err
}
func (s *Store) AgentToken(token string) (domain.AgentToken, error) {
	if len(token) != 43 {
		return domain.AgentToken{}, domain.ErrNotFound
	}
	return scanToken(s.db.QueryRow("SELECT id,name,role,scope,expires,created_at FROM agent_tokens WHERE token_hash=? AND expires>?", security.Digest(token), time.Now().Unix()))
}
func (s *Store) AgentTokens() ([]domain.AgentToken, error) {
	rows, err := s.db.Query("SELECT id,name,role,scope,expires,created_at FROM agent_tokens ORDER BY created_at DESC LIMIT 32")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AgentToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) RevokeAgentToken(id string) error {
	_, err := s.db.Exec("DELETE FROM agent_tokens WHERE id=?", id)
	if err != nil {
		return err
	}
	return s.AddEvent("info", "agent.token_revoked", "Agent令牌已撤销", "")
}

type privateFleet struct {
	Host  domain.FleetHost `json:"host"`
	Token string           `json:"token"`
}

func (s *Store) FleetHost(id string) (domain.FleetHost, error) {
	if len(id) != 16 {
		return domain.FleetHost{}, domain.ErrNotFound
	}
	var encoded string
	if err := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", "fleet-v1:"+id).Scan(&encoded); err != nil {
		return domain.FleetHost{}, err
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return domain.FleetHost{}, err
	}
	v, err := s.secretVault()
	if err != nil {
		return domain.FleetHost{}, err
	}
	instance, err := s.InstanceID()
	if err != nil {
		return domain.FleetHost{}, err
	}
	raw, err := v.Open("miku-fleet-v1:"+instance+":"+id, sealed)
	if err != nil {
		return domain.FleetHost{}, err
	}
	defer clear(raw)
	var p privateFleet
	if json.Unmarshal(raw, &p) != nil || p.Host.ID != id || len(p.Token) != 43 {
		return domain.FleetHost{}, errors.New("远端记录无法认证")
	}
	p.Host.Token = p.Token
	return p.Host, nil
}
func (s *Store) FleetHosts() ([]domain.FleetHost, error) {
	rows, err := s.db.Query("SELECT key FROM metadata WHERE key LIKE 'fleet-v1:%' ORDER BY key LIMIT 33")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, strings.TrimPrefix(key, "fleet-v1:"))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 32 {
		return nil, errors.New("远端数量超过32")
	}
	out := []domain.FleetHost{}
	for _, id := range ids {
		h, err := s.FleetHost(id)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}
func (s *Store) SaveFleetHost(h domain.FleetHost) (domain.FleetHost, error) {
	if h.ID == "" {
		hosts, err := s.FleetHosts()
		if err != nil {
			return h, err
		}
		if len(hosts) >= 32 {
			return h, errors.New("最多32台远端")
		}
		h.ID, err = security.Token(12)
		if err != nil {
			return h, err
		}
	} else {
		if _, err := s.FleetHost(h.ID); err != nil {
			return h, err
		}
	}
	h.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(privateFleet{h, h.Token})
	if err != nil {
		return h, err
	}
	defer clear(raw)
	v, err := s.secretVault()
	if err != nil {
		return h, err
	}
	instance, err := s.InstanceID()
	if err != nil {
		return h, err
	}
	sealed, err := v.Seal("miku-fleet-v1:"+instance+":"+h.ID, raw)
	if err != nil {
		return h, err
	}
	_, err = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "fleet-v1:"+h.ID, base64.StdEncoding.EncodeToString(sealed))
	h.Token = ""
	return h, err
}
func (s *Store) DeleteFleetHost(id string) error {
	_, err := s.db.Exec("DELETE FROM metadata WHERE key=?", "fleet-v1:"+id)
	return err
}
func (s *Store) SnapshotDatabase(ctx context.Context, destination string) error {
	if strings.ContainsRune(destination, '\x00') {
		return errors.New("备份路径无效")
	}
	_, err := s.db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(destination, "'", "''")+"'")
	return err
}
