package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/vpngate"
	"time"
)

func (s *Store) Recovery(id string) (domain.RecoveryPolicy, error) {
	if _, _, e := s.ExitForRuntime(id); e != nil {
		return domain.RecoveryPolicy{}, e
	}
	var raw string
	e := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", "recovery-v1:"+id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.RecoveryPolicy{NodeIDs: []string{}}, nil
	}
	if e != nil {
		return domain.RecoveryPolicy{}, e
	}
	var p domain.RecoveryPolicy
	if json.Unmarshal([]byte(raw), &p) != nil {
		return p, errors.New("自动恢复设置无法读取")
	}
	p.Confirm = true
	if p.Validate() != nil {
		return p, errors.New("自动恢复设置未知")
	}
	p.Confirm = false
	return p, nil
}
func (s *Store) SaveRecovery(id string, p domain.RecoveryPolicy) error {
	if e := p.Validate(); e != nil {
		return e
	}
	exit, _, e := s.ExitForRuntime(id)
	if e != nil {
		return e
	}
	if p.Enabled && exit.Policy == "residential_strict" {
		return errors.New("没有可核验住宅依据，自动恢复不绕过严格住宅策略")
	}
	if p.Enabled && len(p.NodeIDs) > 0 {
		if _, _, upstream, e := s.Upstream(id); e != nil || upstream {
			return errors.New("代理上游模式不能自动改为VPN候选，请先明确切换出口配置")
		}
		for _, id := range p.NodeIDs {
			if blocked, e := s.NodeBlocked(id); e != nil || blocked {
				return errors.New("自动恢复候选包含已屏蔽节点")
			}
			node, e := s.NodeForRuntime(id)
			if e != nil || node.Source != vpngate.Source || node.Country != exit.Country || node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
				return errors.New("候选必须来自当前官方缓存且同出口国家，不跨国家自动换线")
			}
		}
	}
	p.Confirm = false
	if p.NodeIDs == nil {
		p.NodeIDs = []string{}
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "recovery-v1:"+id, string(raw))
	return e
}

// Reducing a permission must work even when its old candidates have expired.
// Update one JSON flag atomically so simultaneous exit/ingress pauses cannot
// restore each other's permissions from an older read-modify-write snapshot.
func (s *Store) pauseRecoveryFlag(id, field string) error {
	if field != "enabled" && field != "resume_ingress" {
		return errors.New("未知恢复授权")
	}
	p, e := s.Recovery(id)
	if e != nil {
		return e
	}
	if field == "enabled" {
		p.Enabled = false
	} else {
		p.ResumeIngress = false
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(`INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=json_set(metadata.value,?,json('false'))`, "recovery-v1:"+id, string(raw), "$."+field)
	return e
}
func (s *Store) PauseRecovery(id string) error { return s.pauseRecoveryFlag(id, "enabled") }
func (s *Store) PauseIngressRecovery(id string) error {
	return s.pauseRecoveryFlag(id, "resume_ingress")
}
