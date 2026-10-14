package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/vpngate"
)

func (s *Store) StandbyPolicy(id string) (domain.StandbyPolicy, error) {
	if _, _, err := s.ExitForRuntime(id); err != nil {
		return domain.StandbyPolicy{}, err
	}
	var raw string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", "standby-v1:"+id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.StandbyPolicy{}, nil
	}
	var p domain.StandbyPolicy
	if err == nil {
		err = json.Unmarshal([]byte(raw), &p)
	}
	return p, err
}
func (s *Store) SaveStandby(id string, p domain.StandbyPolicy) error {
	if !p.Confirm {
		return errors.New("需确认热备会额外使用一条真实VPN会话")
	}
	exit, _, err := s.ExitForRuntime(id)
	if err != nil {
		return err
	}
	if p.Enabled {
		if exit.Policy == "residential_strict" {
			return errors.New("没有住宅证据时不绕过严格策略")
		}
		config, ok, err := s.VPN(id)
		if err != nil || !ok {
			return errors.New("先配置主OpenVPN节点")
		}
		if config.NodeID == p.NodeID {
			return errors.New("备用必须与主节点不同")
		}
		node, err := s.NodeForRuntime(p.NodeID)
		if err != nil || node.Country != exit.Country || node.Source != vpngate.Source || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
			return errors.New("备用需为新鲜官方同国家节点")
		}
		blocked, err := s.NodeBlocked(node.ID)
		if err != nil || blocked {
			return errors.New("备用节点已屏蔽")
		}
	} else {
		p.NodeID = ""
		p.AllowLegacy = false
	}
	p.Confirm = false
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "standby-v1:"+id, string(raw))
	return err
}
