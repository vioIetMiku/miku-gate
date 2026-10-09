package tunnel

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"mikugate/internal/egress"
)

// ProfileCheck is a format/policy report, NEVER an authorization or health proof.
// It carries no CA, certificate, private key, or normalized executable config.
type ProfileCheck struct {
	Mode                     string    `json:"mode"`
	StrictCompatible         bool      `json:"strict_compatible"`
	LegacyCandidate          bool      `json:"legacy_candidate"`
	LegacyDataChannel        bool      `json:"legacy_data_channel"`
	SharedClientIdentity     bool      `json:"shared_client_identity"`
	ExpectedPeerNameRequired bool      `json:"expected_peer_name_required"`
	ServerIdentityVerified   bool      `json:"server_identity_verified"`
	NetworkTested            bool      `json:"network_tested"`
	ProfileSHA256            string    `json:"profile_sha256,omitempty"`
	TLSMinimum               string    `json:"tls_minimum"`
	Message                  string    `json:"message"`
	CheckedAt                time.Time `json:"checked_at"`
}

func CheckProfile(raw, expectedIP string) ProfileCheck {
	result := ProfileCheck{Mode: "unsupported", TLSMinimum: "1.2", ExpectedPeerNameRequired: true, CheckedAt: time.Now().UTC(), Message: "配置未通过受限格式/证书策略；未执行、未联网，不能作为可用节点。"}
	if len(raw) == 0 || len(raw) > MaxProfileBytes {
		return result
	}
	sum := sha256.Sum256([]byte(raw))
	result.ProfileSHA256 = hex.EncodeToString(sum[:])
	if _, err := Normalize(raw, expectedIP); err == nil {
		result.Mode = "strict"
		result.StrictCompatible = true
		result.Message = "配置符合严格AEAD策略。仅为静态检查；服务器身份、连通性及出口IP尚未验证。"
		return result
	}
	// Static inspection uses a deliberately non-deployable analysis-only peer
	// label if none was supplied. This profile value is discarded, never returned
	// or started. Runtime callers must provide a trusted expected server name.
	peerName := "miku-static-analysis-not-a-peer"
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), MaxProfileBytes)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "verify-x509-name" && fields[2] == "name" && peerNamePattern.MatchString(fields[1]) {
			peerName = fields[1]
			break
		}
	}
	profile, err := normalizeOptions(raw, expectedIP, result.CheckedAt, egress.PublicIPv4, NormalizeOptions{AllowVPNGateLegacy: true, ExpectedPeerName: peerName})
	if err == nil {
		result.Mode = "legacy-candidate"
		result.LegacyCandidate = true
		result.LegacyDataChannel = profile.LegacyDataChannel()
		result.SharedClientIdentity = profile.SharedClientIdentity()
		result.Message = "配置属于VPN Gate兼容候选，需要明确同意旧数据通道，并绑定可信的服务器名称。共享客户端身份不是服务器信任根；尚未连接或验证出口。"
	}
	return result
}
