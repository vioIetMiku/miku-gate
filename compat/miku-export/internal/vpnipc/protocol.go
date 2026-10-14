// Package vpnipc is the separately authorized local OpenVPN session protocol.
// It is not the quarantine sandbox protocol and accepts no programs or paths.
package vpnipc

import (
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/netexec"
	"mikugate/internal/tunnel"
	"regexp"
)

const Version = 1
const MaxRequest = 192 << 10
const MaxResponse = 4096

var ErrUnavailable = errors.New("隔离OpenVPN执行器不可用或会话已失效")
var ErrProfile = errors.New("VPN配置/服务器身份不满足受限策略")
var ErrBusy = errors.New("VPN执行器繁忙或此出口已有会话")
var ErrProtocol = errors.New("VPN执行器响应无效")
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)
var fingerprintPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Config struct {
	Front           *TransportProxy `json:"-"`
	PeerFingerprint string          `json:"peer_fingerprint"`
	Profile         string          `json:"-"`
	IP              string          `json:"ip"`
	PeerName        string          `json:"peer_name"`
	AllowLegacy     bool            `json:"allow_legacy"`
}

type TransportProxy struct {
	Config   domain.UpstreamConfig `json:"config"`
	Password string                `json:"password"`
}

func (Config) String() string   { return "VPN config (private material redacted)" }
func (Config) GoString() string { return "VPN config (private material redacted)" }

type request struct {
	Front           *TransportProxy `json:"front,omitempty"`
	PeerFingerprint string          `json:"peer_fingerprint,omitempty"`
	Version         int             `json:"version"`
	Operation       string          `json:"operation"`
	ExitID          string          `json:"exit_id,omitempty"`
	Token           string          `json:"token,omitempty"`
	Profile         string          `json:"profile,omitempty"`
	IP              string          `json:"ip,omitempty"`
	PeerName        string          `json:"peer_name,omitempty"`
	Legacy          bool            `json:"legacy,omitempty"`
	Address         string          `json:"address,omitempty"`
}

func (r request) valid() bool {
	if r.Version != Version {
		return false
	}
	if r.Front != nil {
		if r.Operation != "start" {
			return false
		}
		c := r.Front.Config
		c.Password = r.Front.Password
		if c.Validate() != nil {
			return false
		}
	}
	switch r.Operation {
	case "capabilities":
		return r.ExitID == "" && r.Token == "" && r.Profile == "" && r.IP == "" && r.PeerName == "" && r.PeerFingerprint == "" && !r.Legacy && r.Address == ""
	case "start":
		return netexec.ValidateID(r.ExitID) == nil && r.Token == "" && len(r.Profile) > 0 && len(r.Profile) <= tunnel.MaxProfileBytes && len(r.IP) <= 15 && len(r.PeerName) > 0 && len(r.PeerName) <= 128 && (r.PeerFingerprint == "" || fingerprintPattern.MatchString(r.PeerFingerprint)) && r.Address == ""
	case "dial", "dial-udp", "verify", "stop":
		return tokenPattern.MatchString(r.Token) && r.ExitID == "" && r.Profile == "" && r.IP == "" && r.PeerName == "" && r.PeerFingerprint == "" && !r.Legacy && len(r.Address) <= 300 && ((r.Operation == "dial" || r.Operation == "dial-udp") && r.Address != "" || r.Operation != "dial" && r.Operation != "dial-udp" && r.Address == "")
	}
	return false
}

type Capabilities struct {
	Version     int                   `json:"version"`
	Instance    string                `json:"instance"`
	Mode        string                `json:"mode"`
	Options     tunnel.SessionOptions `json:"options"`
	MaxSessions int                   `json:"max_sessions"`
}
type response struct {
	Version      int                 `json:"version"`
	Error        string              `json:"error,omitempty"`
	Token        string              `json:"token,omitempty"`
	Info         *tunnel.SessionInfo `json:"info,omitempty"`
	Capabilities *Capabilities       `json:"capabilities,omitempty"`
}

func resultError(code string) error {
	switch code {
	case "":
		return nil
	case "profile":
		return ErrProfile
	case "busy":
		return ErrBusy
	case "unavailable":
		return ErrUnavailable
	}
	return ErrProtocol
}
