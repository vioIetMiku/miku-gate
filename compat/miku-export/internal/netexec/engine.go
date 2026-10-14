package netexec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Identity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

func (i Identity) valid() bool { return i.Inode != 0 }

type record struct {
	Schema     int       `json:"schema"`
	InstanceID string    `json:"instance_id"`
	ExitID     string    `json:"exit_id"`
	Namespace  string    `json:"namespace"`
	Identity   Identity  `json:"identity"`
	Phase      string    `json:"phase"`
	CreatedAt  time.Time `json:"created_at"`
}

type recordStore interface {
	Load(string) (record, bool, error)
	Save(string, record) error
	Delete(string) error
	Count() (int, error)
}

type backend interface {
	Lookup(context.Context, Plan) (Identity, bool, error)
	Create(context.Context, Plan) (Identity, error)
	InstallGuard(context.Context, Plan, Identity) error
	Verify(context.Context, Plan, Identity) (bool, error)
	Delete(context.Context, Plan, Identity) error
}

type Result struct {
	Plan               Plan   `json:"plan"`
	Phase              string `json:"phase"`
	QuarantineVerified bool   `json:"quarantine_verified"`
	TunnelReady        bool   `json:"tunnel_ready"`
	ProxyListening     bool   `json:"proxy_listening"`
	Reason             string `json:"reason"`
}

type Engine struct {
	mu         sync.Mutex
	instanceID string
	records    recordStore
	backend    backend
	close      func() error
	readOnly   bool
	closed     bool
}

func newEngine(instanceID string, records recordStore, b backend) (*Engine, error) {
	if err := ValidateID(instanceID); err != nil {
		return nil, err
	}
	return &Engine{instanceID: instanceID, records: records, backend: b}, nil
}
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	if e.close == nil {
		return nil
	}
	close := e.close
	e.close = nil
	return close()
}
func result(p Plan, phase, reason string) Result {
	return Result{Plan: p, Phase: phase, Reason: reason}
}

func (e *Engine) observe(ctx context.Context, exitID string) (Plan, record, bool, Identity, bool, error) {
	p, err := BuildPlan(e.instanceID, exitID)
	if err != nil {
		return p, record{}, false, Identity{}, false, err
	}
	r, exists, err := e.records.Load(p.Namespace)
	if err != nil {
		return p, r, exists, Identity{}, false, err
	}
	if exists && (r.Schema != Schema || r.InstanceID != p.InstanceID || r.ExitID != p.ExitID || r.Namespace != p.Namespace || r.CreatedAt.IsZero()) {
		return p, r, exists, Identity{}, false, ErrOwnership
	}
	identity, found, err := e.backend.Lookup(ctx, p)
	return p, r, exists, identity, found, err
}

func (e *Engine) Inspect(ctx context.Context, exitID string) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Result{}, errors.New("sandbox engine is closed")
	}
	return e.inspect(ctx, exitID)
}
func (e *Engine) inspect(ctx context.Context, exitID string) (Result, error) {
	p, r, recorded, identity, found, err := e.observe(ctx, exitID)
	if err != nil {
		return result(p, "unverified", "无法安全读取资源状态。"), err
	}
	if !found {
		if recorded {
			return result(p, "stale-record", "namespace 已不存在；可显式 cleanup 清理本实例记录。"), nil
		}
		return result(p, "absent", "没有本出口的隔离沙箱。"), nil
	}
	if !recorded {
		return result(p, "unowned", "同名 namespace 无本实例归属记录；拒绝接管或删除。"), nil
	}
	if !r.Identity.valid() {
		return result(p, "incomplete", "创建中断，未持久化 namespace 身份；需要管理员人工核验。"), nil
	}
	if r.Identity != identity {
		return result(p, "ownership-conflict", "namespace inode 与记录不符；拒绝接管或删除。"), nil
	}
	verified, err := e.backend.Verify(ctx, p, identity)
	if err != nil {
		return result(p, "unverified", "无法验证当前内核隔离规则。"), err
	}
	if !verified {
		return result(p, "drifted", "规则或接口与隔离计划不符；不报告已保护，不自动重写。"), nil
	}
	out := result(p, "quarantined", "已核对独立 namespace 和全流量丢弃规则；未创建隧道或代理。")
	out.QuarantineVerified = true
	return out, nil
}

func (e *Engine) Prepare(ctx context.Context, exitID string) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.readOnly {
		return Result{}, errors.New("sandbox engine is closed or read-only")
	}
	current, err := e.inspect(ctx, exitID)
	if err != nil {
		return current, err
	}
	if current.Phase == "quarantined" {
		return current, nil
	}
	if current.Phase != "absent" {
		return current, ErrConflict
	}
	p := current.Plan
	count, err := e.records.Count()
	if err != nil {
		return current, err
	}
	if count >= MaxSandboxes {
		return current, ErrLimit
	}
	r := record{Schema: Schema, InstanceID: p.InstanceID, ExitID: p.ExitID, Namespace: p.Namespace, Phase: "preparing", CreatedAt: time.Now().UTC()}
	// Intent is persisted before creation. A crash before the inode is saved is
	// deliberately treated as ambiguous, not as permission to remove a namespace.
	if err = e.records.Save(p.Namespace, r); err != nil {
		return current, err
	}
	identity, err := e.backend.Create(ctx, p)
	if err != nil {
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, found, lookupErr := e.backend.Lookup(check, p)
		if lookupErr == nil && !found {
			err = errors.Join(err, e.records.Delete(p.Namespace))
		}
		return result(p, "incomplete", "创建失败；无法证明归属的资源会被保留，未接入任何业务流量。"), errors.Join(err, lookupErr)
	}
	if !identity.valid() {
		return result(p, "incomplete", "执行器未返回有效 namespace 身份。"), ErrOwnership
	}
	r.Identity, r.Phase = identity, "created"
	if err = e.records.Save(p.Namespace, r); err != nil {
		return e.rollback(p, r, err)
	}
	if err = e.backend.InstallGuard(ctx, p, identity); err != nil {
		return e.rollback(p, r, err)
	}
	verified, err := e.backend.Verify(ctx, p, identity)
	if err != nil || !verified {
		return e.rollback(p, r, errors.Join(ErrUnverified, err))
	}
	r.Phase = "quarantined"
	if err = e.records.Save(p.Namespace, r); err != nil {
		return e.rollback(p, r, err)
	}
	out := result(p, "quarantined", "已创建并核验全阻断沙箱；尚无 VPN、DNS 拨号器或代理监听。")
	out.QuarantineVerified = true
	return out, nil
}

func (e *Engine) rollback(p Plan, r record, cause error) (Result, error) {
	// Request cancellation must not skip cleanup, but cleanup has its own bound.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.backend.Delete(ctx, p, r.Identity); err != nil {
		r.Phase = "cleanup-required"
		saveErr := e.records.Save(p.Namespace, r)
		return result(p, "cleanup-required", "回滚未完成；归属记录已保留，请检查后显式 cleanup。"), errors.Join(cause, fmt.Errorf("rollback: %w", err), saveErr)
	}
	if err := e.records.Delete(p.Namespace); err != nil {
		return result(p, "stale-record", "namespace 已回收，但归属记录清理失败。"), errors.Join(cause, err)
	}
	return result(p, "absent", "准备失败，已回收本次创建的 namespace；没有业务监听。"), cause
}

func (e *Engine) Cleanup(ctx context.Context, exitID string) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.readOnly {
		return Result{}, errors.New("sandbox engine is closed or read-only")
	}
	p, r, recorded, identity, found, err := e.observe(ctx, exitID)
	if err != nil {
		return result(p, "unverified", "读取失败，未执行清理。"), err
	}
	if !found {
		if recorded {
			if err = e.records.Delete(p.Namespace); err != nil {
				return result(p, "stale-record", "记录清理失败。"), err
			}
		}
		return result(p, "absent", "namespace 不存在；本出口残留记录已清理（若有）。"), nil
	}
	if !recorded || !r.Identity.valid() || r.Identity != identity {
		return result(p, "ownership-conflict", "无法证明 namespace 归属，未执行删除。"), ErrOwnership
	}
	if err = e.backend.Delete(ctx, p, identity); err != nil {
		return result(p, "cleanup-required", "清理被拒绝或失败；不会杀死进程、移动网卡或清理陌生资源。"), err
	}
	if err = e.records.Delete(p.Namespace); err != nil {
		return result(p, "stale-record", "namespace 已回收，记录清理失败。"), err
	}
	return result(p, "absent", "仅回收了经 inode 校验的本出口 namespace 和归属记录。"), nil
}
