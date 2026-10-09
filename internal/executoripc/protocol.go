// Package executoripc exposes only the quarantine sandbox over a local,
// authenticated Unix socket. It never accepts commands, filenames or VPN configs.
package executoripc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"reflect"
	"regexp"
	"time"

	"mikugate/internal/netexec"
)

const Version = 1
const SocketRoot = "/run/miku-gate-ipc"
const MaxRequestBytes = 4096
const MaxResponseBytes = 32768
const MaxConnections = 4
const OperationTimeout = 20 * time.Second
const CallTimeout = 32 * time.Second
const IOTimeout = 3 * time.Second

var ErrUnsupported = errors.New("sandbox IPC requires Linux")
var ErrUnavailable = errors.New("sandbox IPC is unavailable; verify the daemon and its configured UID")
var ErrPeer = errors.New("Unix socket peer or filesystem ownership verification failed")
var ErrProtocol = errors.New("invalid sandbox IPC protocol")
var requestIDPattern = regexp.MustCompile("^[a-f0-9]{32}$")

type Runtime interface {
	Inspect(context.Context, string) (netexec.Result, error)
	Prepare(context.Context, string) (netexec.Result, error)
	Cleanup(context.Context, string) (netexec.Result, error)
}

type Capabilities struct {
	Version          int    `json:"version"`
	InstanceID       string `json:"instance_id"`
	Mode             string `json:"mode"`
	SandboxAvailable bool   `json:"sandbox_available"`
	VPNAvailable     bool   `json:"vpn_available"`
	ProxyAvailable   bool   `json:"proxy_available"`
	MaxConnections   int    `json:"max_connections"`
}

func capabilities(instance string) Capabilities {
	return Capabilities{Version: Version, InstanceID: instance, Mode: "quarantine-only", SandboxAvailable: true, MaxConnections: MaxConnections}
}

type Request struct {
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Operation string `json:"operation"`
	ExitID    string `json:"exit_id,omitempty"`
	Apply     bool   `json:"apply,omitempty"`
}

func (r Request) validate() error {
	if r.Version != Version || !requestIDPattern.MatchString(r.ID) {
		return ErrProtocol
	}
	switch r.Operation {
	case "capabilities":
		if r.ExitID != "" || r.Apply {
			return ErrProtocol
		}
	case "inspect":
		if r.Apply || netexec.ValidateID(r.ExitID) != nil {
			return ErrProtocol
		}
	case "prepare", "cleanup":
		if !r.Apply || netexec.ValidateID(r.ExitID) != nil {
			return ErrProtocol
		}
	default:
		return ErrProtocol
	}
	return nil
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Code + ": " + e.Message }
func rpcError(code string) *RPCError {
	messages := map[string]string{
		"invalid_request":    "请求不符合限定的隔离协议。",
		"busy":               "执行器忙，请稍后先查询实际状态。",
		"ownership_conflict": "资源归属不符，未接管或删除陌生资源。",
		"conflict":           "存在不完整或冲突状态，请先检查。",
		"limit":              "隔离沙箱达到资源预算。",
		"unverified":         "无法证明隔离状态，不能报告已保护。",
		"canceled":           "操作已取消；重试前请查询实际状态。",
		"timeout":            "操作超时；不能假设已经回滚，请查询实际状态。",
		"operation_failed":   "隔离操作失败；请查看实际状态，不会返回外部命令输出。",
	}
	message, ok := messages[code]
	if !ok {
		return nil
	}
	return &RPCError{Code: code, Message: message}
}
func classify(err error) *RPCError {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return rpcError("timeout")
	case errors.Is(err, context.Canceled):
		return rpcError("canceled")
	case errors.Is(err, netexec.ErrBusy):
		return rpcError("busy")
	case errors.Is(err, netexec.ErrOwnership):
		return rpcError("ownership_conflict")
	case errors.Is(err, netexec.ErrConflict):
		return rpcError("conflict")
	case errors.Is(err, netexec.ErrLimit):
		return rpcError("limit")
	case errors.Is(err, netexec.ErrUnverified):
		return rpcError("unverified")
	default:
		return rpcError("operation_failed")
	}
}

type response struct {
	Version      int             `json:"version"`
	ID           string          `json:"id"`
	Capabilities *Capabilities   `json:"capabilities,omitempty"`
	Result       *netexec.Result `json:"result,omitempty"`
	Error        *RPCError       `json:"error,omitempty"`
}

func SocketPath(instance string) (string, error) {
	if err := netexec.ValidateID(instance); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("miku-gate/ipc/v1\x00" + instance))
	return path.Join(SocketRoot, hex.EncodeToString(sum[:12]), "control.sock"), nil
}
func validResult(instance, exit string, result netexec.Result) bool {
	plan, err := netexec.BuildPlan(instance, exit)
	if err != nil || !reflect.DeepEqual(result.Plan, plan) || result.TunnelReady || result.ProxyListening || len(result.Reason) > 1024 {
		return false
	}
	switch result.Phase {
	case "absent", "stale-record", "unowned", "incomplete", "ownership-conflict", "drifted", "cleanup-required", "unverified":
		return !result.QuarantineVerified
	case "quarantined":
		return result.QuarantineVerified
	default:
		return false
	}
}

// Framing is length-prefixed; a connection carries exactly one request/reply.
func readFrame(r io.Reader, max int) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header[:]))
	if size < 2 || size > max {
		return nil, ErrProtocol
	}
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return data, err
}
func writeFrame(w io.Writer, value any, max int) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > max {
		return ErrProtocol
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	_, err = io.Copy(w, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(raw)))
	return err
}
func decode(raw []byte, value any) error {
	if !uniqueJSON(raw) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrProtocol
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrProtocol
	}
	return nil
}

// Reject ambiguous duplicate keys and excessive nesting before typed decoding.
func uniqueJSON(raw []byte) bool {
	if len(raw) > MaxResponseBytes {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	count := 0
	var walk func(int) bool
	walk = func(depth int) bool {
		count++
		if depth > 12 || count > 4096 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return depth > 0
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				for _, c := range name {
					if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
						return false
					}
				}
				seen[name] = true
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			if depth == 0 {
				return false
			}
			for d.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !walk(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
