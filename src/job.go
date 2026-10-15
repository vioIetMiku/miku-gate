package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// JobStep 是作业里的一个目标。界面按步骤逐条显示，用户能看清是哪一个卡住了。
type JobStep struct {
	Label  string `json:"label"`
	Status string `json:"status"` // pending | running | ok | failed
	Detail string `json:"detail"`
}

// Job 是一次批量开出口的作业。
type Job struct {
	mu          sync.Mutex
	id          string
	summary     string
	status      string
	steps       []*JobStep
	started     time.Time
	ended       time.Time
	resultURL   string
	kind        string
	quickResult *QuickSetupResult
}

// JobView 是 Job 的只读快照，用于返回给界面。
type JobView struct {
	QuickResult *QuickSetupResult `json:"quick_result,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	ResultURL   string            `json:"result_url,omitempty"`
	ID          string            `json:"id"`
	Summary     string            `json:"summary"`
	Status      string            `json:"status"` // running | done | failed
	Steps       []JobStep         `json:"steps"`
	Started     time.Time         `json:"started"`
	Done        int               `json:"done"`
	Total       int               `json:"total"`
}

func (j *Job) SetQuickResult(result QuickSetupResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	result.Regions = append([]QuickRegionResult(nil), result.Regions...)
	j.quickResult = &result
}

func (j *Job) ID() string { return j.id }

func (j *Job) SetKind(kind string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.kind = kind
}

func (j *Job) failUnfinished(detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, step := range j.steps {
		if step.Status == "pending" || step.Status == "running" {
			step.Status, step.Detail = "failed", detail
		}
	}
}

func (j *Job) SetResultURL(url string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.resultURL = url
}

// Set 更新某一步的状态。
func (j *Job) Set(i int, status, detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if i < 0 || i >= len(j.steps) {
		return
	}
	j.steps[i].Status = status
	j.steps[i].Detail = detail
}

// Finish 收尾：只要有一步失败就整体标记为 failed，界面据此决定是否留着让用户看。
func (j *Job) Finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = "done"
	for _, s := range j.steps {
		if s.Status == "failed" {
			j.status = "failed"
			break
		}
	}
	j.ended = time.Now()
}

func (j *Job) View() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{
		Kind:      j.kind,
		ResultURL: j.resultURL,
		ID:        j.id, Summary: j.summary, Status: j.status,
		Started: j.started, Total: len(j.steps),
		Steps: make([]JobStep, 0, len(j.steps)),
	}
	if j.quickResult != nil {
		result := *j.quickResult
		result.Regions = append([]QuickRegionResult(nil), result.Regions...)
		v.QuickResult = &result
	}
	for _, s := range j.steps {
		v.Steps = append(v.Steps, *s)
		if s.Status == "ok" || s.Status == "failed" {
			v.Done++
		}
	}
	return v
}

// JobStore 保存最近的作业。作业本身是瞬时的，进程重启后不需要恢复。
type JobStore struct {
	mu   sync.Mutex
	jobs []*Job
}

// keepJobs 限制保留数量，避免长期运行后无限增长。
const keepJobs = 8

func (s *JobStore) New(summary string, labels []string) *Job {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	j := &Job{
		id: hex.EncodeToString(buf), summary: summary,
		status: "running", started: time.Now(),
	}
	for _, l := range labels {
		j.steps = append(j.steps, &JobStep{Label: l, Status: "pending"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	if len(s.jobs) > keepJobs {
		s.jobs = s.jobs[len(s.jobs)-keepJobs:]
	}
	return j
}

// Views 按点击创建的顺序返回，新的追加在末尾。
func (s *JobStore) Views() []JobView {
	s.mu.Lock()
	jobs := make([]*Job, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.Unlock()

	out := make([]JobView, 0, len(jobs))
	for i := 0; i < len(jobs); i++ {
		out = append(out, jobs[i].View())
	}
	return out
}

// Dismiss 丢掉一个已结束的作业，用户点关闭时调用。
func (s *JobStore) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.jobs[:0]
	for _, j := range s.jobs {
		if j.id != id || j.View().Status == "running" {
			kept = append(kept, j)
		}
	}
	s.jobs = kept
}
