package store

import (
	"encoding/json"
	"errors"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/tunnel"
)

func (s *Store) SaveVPNTransport(id string, input *domain.UpstreamInput) error {
	var old []byte
	if err := s.db.QueryRow("SELECT sealed FROM vpn_configs WHERE exit_id=?", id).Scan(&old); err != nil {
		return err
	}
	config, ok, err := s.VPN(id)
	if err != nil || !ok {
		return errors.New("先配置主VPN节点")
	}
	var front *domain.UpstreamConfig
	if input != nil {
		if input.ReplaceVPN {
			return errors.New("前置通道与最终出口分开配置")
		}
		if err = input.Validate(); err != nil {
			return err
		}
		value := input.Config()
		exit, _, err := s.ExitForRuntime(id)
		if err != nil {
			return err
		}
		if value.Host == "127.0.0.1" && value.Port == exit.Port {
			return errors.New("前置代理不能指向本出口自己的入口")
		}
		if input.ReusePassword {
			if config.FrontProxy == nil || !value.SameCredentialTarget(*config.FrontProxy) {
				return errors.New("仅同前置目标可复用密码")
			}
			value.Password = config.FrontProxy.Password
		}
		if err = value.Validate(); err != nil {
			return err
		}
		normalized, err := tunnel.NormalizeWithOptions(config.Profile, config.IP, tunnel.NormalizeOptions{AllowVPNGateLegacy: config.AllowLegacy, ExpectedPeerName: config.PeerName, ExpectedPeerFingerprint: config.PeerFingerprint})
		if err != nil || normalized.Endpoint().Transport != "tcp4-client" {
			return errors.New("HTTP/SOCKS前置通道仅支持OpenVPN TCP；UDP不伪装成可用")
		}
		front = &value
	}
	config.UpdatedAt = time.Now().UTC()
	plain, err := json.Marshal(protectedVPN{Config: config, Profile: config.Profile, Front: front, FrontPassword: func() string {
		if front != nil {
			return front.Password
		}
		return ""
	}()})
	if err != nil {
		return err
	}
	defer clear(plain)
	v, err := s.secretVault()
	if err != nil {
		return err
	}
	label, err := s.vpnLabel(id)
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
	result, err := tx.Exec("UPDATE vpn_configs SET sealed=? WHERE exit_id=? AND sealed=?", sealed, id, old)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return domain.ErrConflict
	}
	if _, err = tx.Exec("DELETE FROM metadata WHERE key=?", "standby-v1:"+id); err != nil {
		return err
	}
	if err = event(tx, "info", "exit.vpn_transport_updated", "已更新VPN底层传输通道，最终业务仍经隔离VPN；未启动", id); err != nil {
		return err
	}
	return tx.Commit()
}
