package tunnel

import (
	"errors"
	"fmt"
	"strings"
)

var ErrSession = errors.New("OpenVPN隔离会话不可用或已失效")
var ErrPrivilege = errors.New("VPN服务需要root监督进程与两个不同的非root工作UID")

type SessionOptions struct {
	TunnelUID uint32 `json:"tunnel_uid"`
	WorkerUID uint32 `json:"worker_uid"`
}

func (o SessionOptions) Validate() error {
	if o.TunnelUID < 1 || o.WorkerUID < 1 || o.TunnelUID >= 1<<31 || o.WorkerUID >= 1<<31 || o.TunnelUID == o.WorkerUID {
		return ErrPrivilege
	}
	return nil
}

type SessionInfo struct {
	Namespace      NamespaceID `json:"namespace"`
	InterfaceIndex int         `json:"interface_index"`
	TunnelUID      uint32      `json:"tunnel_uid"`
	WorkerUID      uint32      `json:"worker_uid"`
	GuardSHA256    string      `json:"guard_sha256"`
}

func (s SessionInfo) String() string {
	return fmt.Sprintf("isolated OpenVPN session (worker_uid=%d)", s.WorkerUID)
}

// FailureCode exposes a fixed diagnostic category, never peer log lines,
// certificate/configuration contents, host paths or remote-provided messages.
func FailureCode(err error) string {
	if err == nil {
		return "none"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "ca md too weak"), strings.Contains(text, "ee key too small"):
		return "certificate_algorithm_rejected"
	case strings.Contains(text, "fingerprint"):
		return "server_fingerprint_rejected"
	case strings.Contains(text, "x509name"):
		return "server_name_rejected"
	case strings.Contains(text, "certificate verify failed"), strings.Contains(text, "verify error"):
		return "server_certificate_rejected"
	case strings.Contains(text, "options error"):
		return "openvpn_option_rejected"
	case strings.Contains(text, "permission denied"), errors.Is(err, ErrPrivilege):
		return "privilege_rejected"
	case strings.Contains(text, "initialization timeout"):
		return "handshake_timeout"
	}
	return "session_unavailable"
}
