package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var vpnPeer = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type VPNInput struct {
	NodeID      string `json:"node_id"`
	PeerName    string `json:"peer_name"`
	AllowLegacy bool   `json:"allow_legacy"`
	Confirm     bool   `json:"confirm"`
}

func (in *VPNInput) Validate() error {
	in.PeerName = strings.TrimSpace(in.PeerName)
	if !in.Confirm {
		return errors.New("请明确确认使用此节点、服务器名称及所选兼容政策；会替换本出口原有上游")
	}
	if len(in.NodeID) < 1 || len(in.NodeID) > 128 || strings.ContainsAny(in.NodeID, "\x00\r\n /\\") {
		return errors.New("节点ID无效")
	}
	if !vpnPeer.MatchString(in.PeerName) {
		return errors.New("必须明确填写来自可信来源的服务器证书名称，不能留空或含通配符")
	}
	return nil
}

type VPNConfig struct {
	Configured    bool      `json:"configured"`
	NodeID        string    `json:"node_id"`
	NodeHostname  string    `json:"node_hostname"`
	IP            string    `json:"ip"`
	PeerName      string    `json:"peer_name"`
	AllowLegacy   bool      `json:"allow_legacy"`
	ProfileSHA256 string    `json:"profile_sha256"`
	Source        string    `json:"source"`
	FetchedAt     time.Time `json:"fetched_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Profile       string    `json:"-"`
}

func (VPNConfig) String() string   { return "VPN binding (configuration material redacted)" }
func (VPNConfig) GoString() string { return "VPN binding (configuration material redacted)" }
