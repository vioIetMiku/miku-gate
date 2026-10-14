// Package tasks runs bounded, cancellable batches of predefined operations.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"mikugate/internal/security"
)

type Item struct {
	ExitID  string `json:"exit_id"`
	Phase   string `json:"phase"`
	Message string `json:"message"`
}
type Job struct {
	ID        string     `json:"id"`
	Action    string     `json:"action"`
	Phase     string     `json:"phase"`
	Items     []Item     `json:"items"`
	CreatedAt time.Time  `json:"created_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
type Source interface {
	SaveTask(string, []byte) error
	TaskRecords() ([][]byte, error)
}
type Operation func(context.Context, string, string) error
type entry struct {
	job    Job
	ctx    context.Context
	cancel context.CancelFunc
}
type Manager struct {
	mu        sync.Mutex
	ctx       context.Context
	source    Source
	operation Operation
	jobs      map[string]*entry
	busy      map[string]bool
	slots     chan struct{}
	wg        sync.WaitGroup
}

func New(ctx context.Context, source Source, operation Operation) (*Manager, error) {
	m := &Manager{ctx: ctx, source: source, operation: operation, jobs: map[string]*entry{}, busy: map[string]bool{}, slots: make(chan struct{}, 2)}
	records, err := source.TaskRecords()
	if err != nil {
		return nil, err
	}
	for _, raw := range records {
		var j Job
		if json.Unmarshal(raw, &j) != nil {
			return nil, errors.New("任务历史无效")
		}
		if j.Phase == "queued" || j.Phase == "running" {
			j.Phase = "interrupted"
			now := time.Now().UTC()
			j.EndedAt = &now
			for i := range j.Items {
				if j.Items[i].Phase == "queued" || j.Items[i].Phase == "running" {
					j.Items[i].Phase = "interrupted"
					j.Items[i].Message = "控制服务已重启，未自动重放操作"
				}
			}
			raw, _ = json.Marshal(j)
			if err = source.SaveTask(j.ID, raw); err != nil {
				return nil, err
			}
		}
		m.jobs[j.ID] = &entry{job: j}
	}
	return m, nil
}
func clone(j Job) Job { j.Items = append([]Item{}, j.Items...); return j }
func (m *Manager) persist(e *entry) error {
	raw, err := json.Marshal(e.job)
	if err != nil {
		return err
	}
	return m.source.SaveTask(e.job.ID, raw)
}
func (m *Manager) Submit(action string, ids []string) (Job, error) {
	if action != "start" && action != "stop" && action != "probe" && action != "start-ingress" {
		return Job{}, errors.New("仅支持启动、停止、检测和启动加密入口")
	}
	if len(ids) < 1 || len(ids) > 32 {
		return Job{}, errors.New("每批需1–32个出口")
	}
	id, err := security.Token(12)
	if err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return Job{}, errors.New("任务运行时已关闭")
	}
	active := 0
	for _, e := range m.jobs {
		if e.job.Phase == "queued" || e.job.Phase == "running" {
			active++
		}
	}
	if active >= 4 {
		return Job{}, errors.New("最多4个在途批量任务")
	}
	seen := map[string]bool{}
	for _, exit := range ids {
		if seen[exit] || m.busy[exit] {
			return Job{}, errors.New("出口重复或已有批量任务")
		}
		seen[exit] = true
	}
	ctx, cancel := context.WithCancel(m.ctx)
	e := &entry{ctx: ctx, cancel: cancel, job: Job{ID: id, Action: action, Phase: "queued", CreatedAt: time.Now().UTC(), Items: []Item{}}}
	for _, exit := range ids {
		e.job.Items = append(e.job.Items, Item{ExitID: exit, Phase: "queued", Message: "等待执行"})
	}
	if err = m.persist(e); err != nil {
		cancel()
		return Job{}, err
	}
	m.jobs[id] = e
	for _, exit := range ids {
		m.busy[exit] = true
	}
	m.trim()
	m.wg.Add(1)
	go m.run(e)
	return clone(e.job), nil
}
func (m *Manager) trim() {
	if len(m.jobs) <= 40 {
		return
	}
	all := []*entry{}
	for _, e := range m.jobs {
		if e.job.EndedAt != nil {
			all = append(all, e)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].job.CreatedAt.Before(all[j].job.CreatedAt) })
	for _, e := range all {
		if len(m.jobs) <= 40 {
			break
		}
		delete(m.jobs, e.job.ID)
	}
}
func (m *Manager) run(e *entry) {
	defer m.wg.Done()
	defer e.cancel()
	failed := false
	for i := range e.job.Items {
		select {
		case m.slots <- struct{}{}:
		case <-e.ctx.Done():
			m.finish(e, "cancelled")
			return
		}
		if e.ctx.Err() != nil {
			<-m.slots
			m.finish(e, "cancelled")
			return
		}
		m.mu.Lock()
		e.job.Phase = "running"
		e.job.Items[i].Phase = "running"
		e.job.Items[i].Message = "正在执行"
		err := m.persist(e)
		m.mu.Unlock()
		if err == nil {
			ctx, cancel := context.WithTimeout(e.ctx, 40*time.Second)
			err = m.operation(ctx, e.job.Action, e.job.Items[i].ExitID)
			cancel()
		}
		<-m.slots
		m.mu.Lock()
		if err != nil {
			failed = true
			e.job.Items[i].Phase = "failed"
			e.job.Items[i].Message = err.Error()
		} else {
			e.job.Items[i].Phase = "done"
			e.job.Items[i].Message = "操作完成，以实时状态确认当前可用性"
		}
		if err = m.persist(e); err != nil {
			failed = true
			e.job.Items[i].Phase = "failed"
			e.job.Items[i].Message = "操作结束但任务记录无法保存"
		}
		m.mu.Unlock()
	}
	phase := "done"
	if failed {
		phase = "partial-failure"
	}
	if e.ctx.Err() != nil {
		phase = "cancelled"
	}
	m.finish(e, phase)
}
func (m *Manager) finish(e *entry, phase string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.job.Phase = phase
	now := time.Now().UTC()
	e.job.EndedAt = &now
	for i := range e.job.Items {
		if e.job.Items[i].Phase == "queued" {
			e.job.Items[i].Phase = "cancelled"
			e.job.Items[i].Message = "未执行"
		}
		delete(m.busy, e.job.Items[i].ExitID)
	}
	if m.persist(e) != nil {
		e.job.Phase = "record-failed"
	}
}
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Job{}
	for _, e := range m.jobs {
		out = append(out, clone(e.job))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.jobs[id]
	if e == nil {
		return errors.New("任务不存在")
	}
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}
func (m *Manager) Wait() { m.wg.Wait() }
