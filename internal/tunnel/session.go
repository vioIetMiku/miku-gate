package tunnel

import (
	"errors"
	"fmt"
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
