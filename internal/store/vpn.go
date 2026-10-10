package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"mikugate/internal/tunnel"
	"mikugate/internal/vpngate"
	"time"
)

type protectedVPN struct {
	Config  domain.VPNConfig `json:"config"`
	Profile string           `json:"profile"`
}

func (s *Store) vpnLabel(id string) (string, error) {
	instance, err := s.InstanceID()
	return "miku-vpn-v1:" + instance + ":" + id, err
}
func (s *Store) VPN(id string) (domain.VPNConfig, bool, error) {
	var config domain.VPNConfig
	var sealed []byte
	err := s.db.QueryRow("SELECT sealed FROM vpn_configs WHERE exit_id=?", id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		_, _, err = s.ExitForRuntime(id)
		return config, false, err
	}
	if err != nil {
		return config, false, err
	}
	vault, err := s.secretVault()
	if err != nil {
		return config, false, err
	}
	label, err := s.vpnLabel(id)
	if err != nil {
		return config, false, err
	}
	plain, err := vault.Open(label, sealed)
	if err != nil {
		return config, false, err
	}
	defer clear(plain)
	var stored protectedVPN
	if json.Unmarshal(plain, &stored) != nil {
		return config, false, secretstore.ErrCiphertext
	}
	config = stored.Config
	config.Profile = stored.Profile
	sum := sha256.Sum256([]byte(config.Profile))
	if !config.Configured || config.Source != vpngate.Source || len(config.Profile) > tunnel.MaxProfileBytes || hex.EncodeToString(sum[:]) != config.ProfileSHA256 {
		return domain.VPNConfig{}, false, secretstore.ErrCiphertext
	}
	input := domain.VPNInput{NodeID: config.NodeID, PeerName: config.PeerName, Confirm: true, AllowLegacy: config.AllowLegacy}
	if input.Validate() != nil {
		return domain.VPNConfig{}, false, secretstore.ErrCiphertext
	}
	if config.PeerFingerprint != "" {
		if len(config.PeerFingerprint) != 64 || config.VerifiedTLSHost == "" || config.IdentityCheckedAt.IsZero() {
			return domain.VPNConfig{}, false, secretstore.ErrCiphertext
		}
	}
	return config, true, nil
}
func (s *Store) SaveVPN(id string, in domain.VPNInput) error { return s.saveVPN(id, in, nil) }
func (s *Store) SaveVerifiedVPN(id string, in domain.VPNInput, identity domain.NodeIdentity) error {
	return s.saveVPN(id, in, &identity)
}
func (s *Store) saveVPN(id string, in domain.VPNInput, identity *domain.NodeIdentity) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if _, _, err := s.ExitForRuntime(id); err != nil {
		return err
	}
	node, err := s.NodeForRuntime(in.NodeID)
	if err != nil {
		return err
	}
	if node.Source != vpngate.Source {
		return errors.New("只允许使用官方HTTPS来源缓存的节点配置")
	}
	fingerprint := ""
	if in.UseVerifiedIdentity {
		if identity == nil || len(identity.PeerFingerprint) != 64 || !identity.Verified || identity.VPNConnected || identity.Source != vpngate.Source || identity.NodeID != node.ID || identity.IP != node.IP || identity.ProfileSHA256 != vpngate.ProfileDigest(node.Config) || time.Since(identity.CheckedAt) > 5*time.Minute || time.Until(identity.CheckedAt) > time.Minute || !time.Now().Before(identity.NotAfter) {
			return vpngate.ErrIdentity
		}
		in.PeerName = identity.PeerName
		fingerprint = identity.PeerFingerprint
		if expected, err := vpngate.OfficialTLSHostname(node.Hostname); err != nil || expected != identity.TLSHost {
			return vpngate.ErrIdentity
		}
	}
	if _, err = tunnel.NormalizeWithOptions(node.Config, node.IP, tunnel.NormalizeOptions{AllowVPNGateLegacy: in.AllowLegacy, ExpectedPeerName: in.PeerName, ExpectedPeerFingerprint: fingerprint}); err != nil {
		return tunnel.ErrProfile
	}
	if node.FetchedAt.IsZero() || time.Since(node.FetchedAt) > 24*time.Hour || time.Until(node.FetchedAt) > 5*time.Minute {
		return errors.New("候选缓存时间无效或超过24小时，请先刷新节点库")
	}
	sum := sha256.Sum256([]byte(node.Config))
	config := domain.VPNConfig{Configured: true, NodeID: node.ID, NodeHostname: node.Hostname, IP: node.IP, PeerName: in.PeerName, AllowLegacy: in.AllowLegacy, ProfileSHA256: hex.EncodeToString(sum[:]), Source: node.Source, FetchedAt: node.FetchedAt, UpdatedAt: time.Now().UTC()}
	if identity != nil && in.UseVerifiedIdentity {
		config.PeerFingerprint = fingerprint
		config.VerifiedTLSHost = identity.TLSHost
		config.IdentityCheckedAt = identity.CheckedAt
	}
	plain, err := json.Marshal(protectedVPN{Config: config, Profile: node.Config})
	if err != nil {
		return err
	}
	defer clear(plain)
	vault, err := s.secretVault()
	if err != nil {
		return err
	}
	label, err := s.vpnLabel(id)
	if err != nil {
		return err
	}
	sealed, err := vault.Seal(label, plain)
	if err != nil {
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
	if _, err = tx.Exec("INSERT INTO vpn_configs(exit_id,sealed) VALUES(?,?) ON CONFLICT(exit_id) DO UPDATE SET sealed=excluded.sealed", id, sealed); err != nil {
		return err
	}
	if err = event(tx, "info", "exit.vpn_selected", "已显式选择官方来源VPN配置快照及预期服务器名称；尚未连接或验证身份", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteVPN(id string) error {
	if _, _, err := s.ExitForRuntime(id); err != nil {
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
	if err = event(tx, "info", "exit.vpn_removed", "已移除OpenVPN配置绑定；未修改入口凭据", id); err != nil {
		return err
	}
	return tx.Commit()
}
