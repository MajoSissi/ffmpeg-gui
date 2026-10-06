// Package engine turns templates into ffmpeg command lines and runs them in a
// cancellable, observable worker pool.
package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/sysx"
)

// Status is the lifecycle state of a job.
type Status string

// Job statuses.
const (
	StatusPending   Status = "pending"   // 排队中
	StatusPreparing Status = "preparing" // 探测源文件
	StatusFiltered  Status = "filtered"  // 被过滤规则排除
	StatusRunning   Status = "running"   // 转码中
	StatusDone      Status = "done"      // 完成
	StatusWarning   Status = "warning"   // 完成但有警告
	StatusFailed    Status = "failed"    // 失败
	StatusCanceled  Status = "canceled"  // 已取消
	StatusSkipped   Status = "skipped"   // 已存在，跳过
)

// Finished reports whether the job reached a terminal state.
func (s Status) Finished() bool {
	switch s {
	case StatusDone, StatusWarning, StatusFailed, StatusCanceled, StatusSkipped, StatusFiltered:
		return true
	}
	return false
}

// Chinese label for the UI / CSV.
func (s Status) Label() string {
	switch s {
	case StatusPending:
		return "排队中"
	case StatusPreparing:
		return "分析中"
	case StatusFiltered:
		return "已排除"
	case StatusRunning:
		return "处理中"
	case StatusDone:
		return "已完成"
	case StatusWarning:
		return "完成(警告)"
	case StatusFailed:
		return "失败"
	case StatusCanceled:
		return "已取消"
	case StatusSkipped:
		return "已跳过"
	}
	return string(s)
}

// Job is one file flowing through the pipeline. All mutable fields are guarded
// by the embedded mutex; callers must use Snapshot / Apply or the runner's
// helpers instead of touching them directly.
type Job struct {
	ID           string `json:"id"`
	Input        string `json:"input"`
	InputName    string `json:"inputName"`
	Output       string `json:"output"`
	OutputName   string `json:"outputName"`
	SourceRoot   string `json:"sourceRoot"`
	TemplateID   string `json:"templateId"`
	TemplateName string `json:"templateName"`
	// Index is the job's position in the order it was added, starting at 1. It
	// feeds {index} in the naming pattern and must be the same number the queue
	// preview shows: an index the preview computes on the fly and the runner never
	// passes gives two different file names for the same job.
	Index int `json:"index"`

	Status   Status   `json:"status"`
	Message  string   `json:"message"`
	Error    string   `json:"error"`
	Warnings []string `json:"warnings"`

	// Frozen marks a job whose ffmpeg process is currently suspended. The UI needs
	// it to say 「已暂停」 rather than 「处理中」 next to a progress bar that has
	// stopped moving -- from the outside the two look identical, and a bar that
	// silently stalls reads as a hang, not as a pause the user asked for.
	Frozen bool `json:"frozen"`

	Command  string  `json:"command"`
	Progress float64 `json:"progress"` // 0..1

	// live ffmpeg counters
	Speed     float64 `json:"speed"`
	Bitrate   string  `json:"bitrate"`
	Frame     int64   `json:"frame"`
	FPS       float64 `json:"fps"`
	OutTimeMS int64   `json:"outTimeMs"`
	OutBytes  int64   `json:"outBytes"`

	// resulting geometry chosen by the planner (0 = untouched)
	TargetWidth  int  `json:"targetWidth"`
	TargetHeight int  `json:"targetHeight"`
	Resized      bool `json:"resized"`

	Duration float64 `json:"duration"`
	Size     int64   `json:"size"`

	InfoBefore *media.Info `json:"infoBefore"`
	InfoAfter  *media.Info `json:"infoAfter"`

	QueuedAt  time.Time `json:"queuedAt"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	ElapsedMS int64     `json:"elapsedMs"`

	LogTail      []string `json:"logTail"`
	LogLineCount int      `json:"logLineCount"`

	RecordID string `json:"recordId"`

	// mu is a pointer so that a snapshot (a plain value copy) never duplicates
	// a live lock; the snapshot gets a fresh one of its own.
	mu     *sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	// pid is the ffmpeg process while it runs, so 暂停 can freeze the process
	// instead of only holding the queue back, and suspend is the handle that
	// thaws it again. Both live under the runner's mutex rather than the job's:
	// freezing is a queue-wide decision and the job lock is never held across an
	// OS call. Frozen is the one exception -- it is job state the UI reads, so it
	// goes through the job lock like every other visible field.
	pid     int
	suspend *sysx.SuspendedProcess
}

// NewJob builds a pending job for an input file.
func NewJob(input, sourceRoot, templateID, templateName string) *Job {
	base := baseName(input)
	return &Job{
		ID:           newJobID(),
		Input:        input,
		InputName:    base,
		SourceRoot:   sourceRoot,
		TemplateID:   templateID,
		TemplateName: templateName,
		Status:       StatusPending,
		Message:      "排队中",
		QueuedAt:     time.Now(),
		LogTail:      make([]string, 0, 64),
		done:         make(chan struct{}),
		mu:           &sync.Mutex{},
	}
}

// lock / unlock guard the mutable fields of a job. They are deliberately not
// named Lock/Unlock: a type exposing those methods is treated as a lock by the
// copylocks analyser, and a Job snapshot must stay a plain copyable value.
func (j *Job) lock() {
	if j.mu == nil {
		j.mu = &sync.Mutex{}
	}
	j.mu.Lock()
}

// unlock releases the job mutex.
func (j *Job) unlock() {
	if j.mu != nil {
		j.mu.Unlock()
	}
}

// Snapshot returns a copy safe to marshal to the frontend.
func (j *Job) Snapshot() Job {
	j.lock()
	defer j.unlock()
	c := *j
	c.cancel = nil
	c.done = nil
	c.suspend = nil
	c.mu = &sync.Mutex{}
	if j.LogTail != nil {
		c.LogTail = append([]string(nil), j.LogTail...)
	}
	if j.Warnings != nil {
		c.Warnings = append([]string(nil), j.Warnings...)
	}
	return c
}

func (j *Job) appendLogLocked(line string, limit int) {
	j.LogTail = append(j.LogTail, line)
	j.LogLineCount++
	if limit > 0 && len(j.LogTail) > limit {
		j.LogTail = append([]string(nil), j.LogTail[len(j.LogTail)-limit:]...)
	}
}

func (j *Job) addWarningLocked(msg string) {
	if len(j.Warnings) >= 50 {
		return
	}
	for _, w := range j.Warnings {
		if w == msg {
			return
		}
	}
	j.Warnings = append(j.Warnings, msg)
}

// ---------------------------------------------------------------------------
// Queue statistics
// ---------------------------------------------------------------------------

// Stats summarizes the whole queue.
type Stats struct {
	Total    int  `json:"total"`
	Pending  int  `json:"pending"`
	Running  int  `json:"running"`
	Done     int  `json:"done"`
	Warning  int  `json:"warning"`
	Failed   int  `json:"failed"`
	Canceled int  `json:"canceled"`
	Skipped  int  `json:"skipped"`
	Filtered int  `json:"filtered"`
	Paused   bool `json:"paused"`
	// Started is false until the user presses 开始. It is what lets the UI say
	// 「未开始」 instead of claiming a queue that simply has not been launched is
	// "paused" -- and a paused queue must still be resumable, a not-yet-started
	// one is not.
	Started  bool    `json:"started"`
	Workers  int     `json:"workers"`
	Progress float64 `json:"progress"`
}

// Overall returns how far the queue has progressed (0..1).
func (s Stats) Overall() float64 { return s.Progress }

// ApplyResult reports what re-pointing jobs at another template changed.
// Requeued is a subset of Applied: those jobs had already finished and were
// sent back to 排队中 so 开始 re-runs them under the new parameters.
type ApplyResult struct {
	Applied  int `json:"applied"`
	Requeued int `json:"requeued"`
}

// ---------------------------------------------------------------------------
// Event names pushed to the frontend
// ---------------------------------------------------------------------------

// Event names emitted by the runner.
const (
	EventJobUpdate = "job:update"
	EventJobLog    = "job:log"
	EventQueue     = "queue:state"
	EventToast     = "app:toast"
	EventRecord    = "record:new"
)

// LogBatch is a group of log lines emitted together to keep the event rate low.
type LogBatch struct {
	JobID string   `json:"jobId"`
	Lines []string `json:"lines"`
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
