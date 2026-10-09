// Package netexec implements a quarantine-only Linux network sandbox.
// It intentionally cannot start a tunnel, resolve business DNS, or open a proxy.
package netexec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

const Schema = 1
const MaxSandboxes = 32
const RuntimeDir = "/run/miku-gate/netexec"

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
var ErrUnsupported = errors.New("network sandbox operations require Linux")
var ErrRootRequired = errors.New("the isolated sandbox helper requires root; the web server must remain unprivileged")
var ErrConflict = errors.New("sandbox exists in an incompatible or incomplete state; inspect it before cleanup")
var ErrOwnership = errors.New("namespace ownership cannot be proven; refusing to adopt or delete it")
var ErrBusy = errors.New("sandbox contains processes, extra interfaces, or foreign resources; refusing cleanup")
var ErrLimit = errors.New("sandbox limit reached")
var ErrUnverified = errors.New("the quarantine policy could not be verified")

// IDs are never used as executable names, filesystem paths, or shell fragments.
func ValidateID(id string) error {
	if !validID.MatchString(id) {
		return errors.New("instance and exit IDs must contain 8–64 ASCII letters, digits, underscores, or hyphens")
	}
	return nil
}

type Change struct {
	Scope       string `json:"scope"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type Plan struct {
	Schema              int      `json:"schema"`
	InstanceID          string   `json:"instance_id"`
	ExitID              string   `json:"exit_id"`
	Stage               string   `json:"stage"`
	Namespace           string   `json:"namespace"`
	Table               string   `json:"nft_table"`
	Owner               string   `json:"owner"`
	Ruleset             string   `json:"nft_ruleset"`
	HostRoutesChanged   bool     `json:"host_routes_changed"`
	HostFirewallChanged bool     `json:"host_firewall_changed"`
	TunnelEnabled       bool     `json:"tunnel_enabled"`
	ProxyEnabled        bool     `json:"proxy_enabled"`
	Changes             []Change `json:"changes"`
	Warnings            []string `json:"warnings"`
}

// BuildPlan is pure: it does not read the OS, create files, or execute commands.
func BuildPlan(instanceID, exitID string) (Plan, error) {
	if err := ValidateID(instanceID); err != nil {
		return Plan{}, err
	}
	if err := ValidateID(exitID); err != nil {
		return Plan{}, err
	}
	sum := sha256.Sum256([]byte("miku-gate/quarantine/v1\x00" + instanceID + "\x00" + exitID))
	digest := hex.EncodeToString(sum[:])
	p := Plan{Schema: Schema, InstanceID: instanceID, ExitID: exitID,
		Stage: "quarantine-only", Namespace: "mg-" + digest[:20], Table: "mg_" + digest[:20], Owner: "miku-gate/quarantine/v1/" + digest,
		Changes: []Change{
			{"runtime", "ownership-record", "保存 root 专属的资源归属记录与 namespace inode；名称冲突时拒绝接管。"},
			{"isolated-namespace", "create", "创建独立且不连接宿主网络的 namespace；不创建 veth、TUN 或默认路由。"},
			{"isolated-namespace", "default-drop", "在该 namespace 内原子安装 inet input/forward/output 默认丢弃规则，包含 IPv4、IPv6 和回环流量。"},
			{"isolated-namespace", "verify", "读取实际 namespace 身份、接口和 nftables 规则；验证成功才报告 quarantined。"},
		},
		Warnings: []string{
			"这只是隔离沙箱，不是 VPN 连接；不启动 OpenVPN，不监听代理端口。",
			"计划预览不会执行命令。Linux 的 prepare/cleanup 操作必须由管理员显式传入 --apply。",
			"不修改宿主默认路由、全局防火墙、IP forwarding、DNS 或现有服务。",
			"沙箱阻断校验不等于真实隧道的防泄漏验收；控制台启动闸门仍关闭。",
		},
	}
	p.Ruleset = fmt.Sprintf(`table inet %s {
 comment "%s"
 chain input { type filter hook input priority -300; policy drop; counter drop; }
 chain forward { type filter hook forward priority -300; policy drop; counter drop; }
 chain output { type filter hook output priority -300; policy drop; counter drop; }
}
`, p.Table, p.Owner)
	return p, nil
}
