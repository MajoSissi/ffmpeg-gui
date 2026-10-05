package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
	"ffmpeggui/internal/sysx"
)

// Providers lets the runner pull fresh configuration without importing the
// application layer (avoids an import cycle).
type Providers struct {
	Settings func() store.Settings
	Binaries func() media.Binaries
	Template func(id string) (store.Template, bool)
	// GlobalTemplate supplies the defaults every template inherits. It may be nil
	// in tests; the shipped global template is used as a fallback.
	GlobalTemplate func() store.Template
}

// Global returns the global template, never nil.
func (p Providers) Global() store.Template {
	if p.GlobalTemplate == nil {
		return store.DefaultGlobalTemplate()
	}
	g := p.GlobalTemplate()
	if !g.Global {
		// Defensive: a caller wired this to a regular template by mistake. The
		// defaults are still a better answer than an empty template.
		return store.DefaultGlobalTemplate()
	}
	return g
}

// ConcurrencyOrDefault returns the worker ceiling, which is the global
// template's concurrency. A per-template value can lower it but never raise it,
// because the pool cannot grow past this number.
func (p Providers) ConcurrencyOrDefault() int {
	g := p.Global()
	if g.Perf == nil || g.Perf.Concurrency < 1 {
		return 1
	}
	return g.Perf.Concurrency
}

// ---------------------------------------------------------------------------
// Background pre-probing
// ---------------------------------------------------------------------------

// Preprobe fills in file information for queued jobs so the table can show
// resolution and duration before the batch is started. It is safe to call
// repeatedly; work already done is skipped.
func (r *Runner) Preprobe() {
	r.mu.Lock()
	if r.probing {
		r.mu.Unlock()
		return
	}
	prov := r.prov
	var targets []*Job
	for _, j := range r.jobs {
		if !j.Status.Finished() {
			targets = append(targets, j)
		}
	}
	if len(targets) == 0 {
		r.mu.Unlock()
		return
	}
	r.probing = true
	ctx, cancel := context.WithCancel(r.ctx)
	r.probeCancel = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.probing = false
			r.probeCancel = nil
			r.mu.Unlock()
		}()

		bins := prov.Binaries()
		if !bins.Ready() {
			return
		}

		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for _, j := range targets {
			j.lock()
			if j.InfoBefore != nil {
				j.unlock()
				continue
			}
			j.unlock()

			wg.Add(1)
			sem <- struct{}{}
			go func(job *Job) {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				pctx, pcancel := context.WithTimeout(ctx, 90*time.Second)
				info, err := media.ProbeFile(pctx, bins.FFprobe, job.Input)
				pcancel()
				if err != nil || info == nil {
					return
				}
				job.lock()
				job.InfoBefore = info
				job.Size = info.Size
				job.Duration = info.Duration
				job.unlock()
				r.emitJob(job)
			}(j)
		}
		wg.Wait()
	}()
}

// StopPreprobe cancels an in-flight pre-probe pass.
func (r *Runner) StopPreprobe() {
	r.mu.Lock()
	if r.probeCancel != nil {
		r.probeCancel()
	}
	r.mu.Unlock()
}

// Runner owns the job queue and the worker pool.
type Runner struct {
	mu    sync.Mutex
	cond  *sync.Cond
	jobs  []*Job
	index map[string]*Job

	prov     Providers
	emit     func(name string, payload any)
	onRecord func(store.Record)

	paused    bool
	stopping  bool
	workers   int
	ctx       context.Context
	cancelAll context.CancelFunc

	// slots caps per-template parallelism; see throttle.go for why it does not
	// share the queue's condition variable.
	slots *throttle

	keepAwake *sysx.KeepAwake
	awakeOn   bool

	probing     bool
	probeCancel context.CancelFunc

	// queueFilter overrides the template's filter rules for the whole queue. It is
	// nil when the user is not overriding anything. This is the highest-precedence
	// filter: the task page exposes it because "this batch, these files only" is a
	// decision about the queue, not about the template, and forcing the user to edit
	// the template (and every other queue that shares it) would be wrong.
	queueFilter *store.FilterSpec
}

// SetQueueFilter installs (or clears, with nil) the queue-level filter override.
// A non-nil spec replaces the template rules outright rather than merging with them:
// the panel shows one set of numbers, and a half-merged rule set would not describe
// what would actually run.
func (r *Runner) SetQueueFilter(f *store.FilterSpec) {
	r.mu.Lock()
	r.queueFilter = f
	r.mu.Unlock()
}

// queueFilterSpec returns the override, or nil when there is none.
func (r *Runner) queueFilterSpec() *store.FilterSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queueFilter
}

// NewRunner creates an idle runner.
func NewRunner() *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		index:     map[string]*Job{},
		keepAwake: sysx.NewKeepAwake(),
		ctx:       ctx,
		cancelAll: cancel,
		slots:     newThrottle(),
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Configure wires the runner to its environment.
func (r *Runner) Configure(p Providers, emit func(string, any), onRecord func(store.Record)) {
	r.mu.Lock()
	r.prov, r.emit, r.onRecord = p, emit, onRecord
	r.mu.Unlock()
}

// Shutdown stops every worker and releases the sleep inhibitor.
func (r *Runner) Shutdown() {
	r.slots.stop()
	r.mu.Lock()
	r.stopping = true
	r.cancelAll()
	r.cond.Broadcast()
	r.mu.Unlock()
	r.keepAwake.Disable()
}

// ---------------------------------------------------------------------------
// Queue management
// ---------------------------------------------------------------------------

// InputItem is a file or directory the user dropped into the queue.
type InputItem struct {
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	Recursive bool   `json:"recursive"`
}

// MediaExtensions is the whitelist used when expanding directories.
var MediaExtensions = map[string]bool{
	"mp4": true, "mkv": true, "mov": true, "avi": true, "wmv": true, "flv": true,
	"webm": true, "m4v": true, "mpg": true, "mpeg": true, "m2v": true, "ts": true,
	"m2ts": true, "mts": true, "vob": true, "rmvb": true, "rm": true, "3gp": true,
	"3g2": true, "ogv": true, "asf": true, "divx": true, "mxf": true, "f4v": true,
	"mp3": true, "m4a": true, "m4b": true, "aac": true, "flac": true, "wav": true,
	"ogg": true, "oga": true, "opus": true, "wma": true, "ac3": true, "dts": true,
	"aiff": true, "aif": true, "ape": true, "mka": true, "amr": true, "wv": true,
	"mp2": true, "ra": true, "gif": true,
}

// AddInputs expands directories and enqueues every media file found.
// Returns the number of jobs added and any per-item errors.
func (r *Runner) AddInputs(items []InputItem, templateID, templateName string) (int, []string) {
	var added []*Job
	var errs []string

	for _, it := range items {
		p := strings.TrimSpace(it.Path)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if !st.IsDir() {
			added = append(added, NewJob(p, filepath.Dir(p), templateID, templateName))
			continue
		}

		root := p
		if !it.Recursive {
			entries, err := os.ReadDir(p)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !isMediaFile(e.Name()) {
					continue
				}
				added = append(added, NewJob(filepath.Join(p, e.Name()), root, templateID, templateName))
			}
			continue
		}

		walkErr := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != p {
					return filepath.SkipDir
				}
				return nil
			}
			if !isMediaFile(d.Name()) {
				return nil
			}
			added = append(added, NewJob(path, root, templateID, templateName))
			return nil
		})
		if walkErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, walkErr))
		}
	}

	if len(added) == 0 {
		return 0, errs
	}

	r.mu.Lock()
	seen := map[string]bool{}
	for _, j := range r.jobs {
		seen[strings.ToLower(j.Input)] = true
	}
	n := 0
	for _, j := range added {
		if seen[strings.ToLower(j.Input)] {
			continue
		}
		seen[strings.ToLower(j.Input)] = true
		r.jobs = append(r.jobs, j)
		r.index[j.ID] = j
		n++
	}
	r.cond.Broadcast()
	r.mu.Unlock()

	r.emitState()
	return n, errs
}

func isMediaFile(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return MediaExtensions[ext]
}

// Jobs returns a snapshot of the queue in insertion order.
func (r *Runner) Jobs() []Job {
	r.mu.Lock()
	refs := append([]*Job(nil), r.jobs...)
	r.mu.Unlock()

	out := make([]Job, 0, len(refs))
	for _, j := range refs {
		out = append(out, j.Snapshot())
	}
	return out
}

// JobByID returns one snapshot.
func (r *Runner) JobByID(id string) (Job, bool) {
	r.mu.Lock()
	j := r.index[id]
	r.mu.Unlock()
	if j == nil {
		return Job{}, false
	}
	return j.Snapshot(), true
}

// Logs returns the buffered log lines for one job.
func (r *Runner) Logs(id string) []string {
	r.mu.Lock()
	j := r.index[id]
	r.mu.Unlock()
	if j == nil {
		return nil
	}
	j.lock()
	defer j.unlock()
	return append([]string(nil), j.LogTail...)
}

// RemoveJob deletes a job (running jobs are cancelled first).
func (r *Runner) RemoveJob(id string) error {
	r.mu.Lock()
	j := r.index[id]
	if j == nil {
		r.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	if j.Status == StatusRunning || j.Status == StatusPreparing {
		if j.cancel != nil {
			j.cancel()
		}
	}
	for i, x := range r.jobs {
		if x.ID == id {
			r.jobs = append(r.jobs[:i], r.jobs[i+1:]...)
			break
		}
	}
	delete(r.index, id)
	r.mu.Unlock()
	r.emitState()
	return nil
}

// RemoveJobs drops several jobs at once and returns how many were removed.
//
// One lock for the whole batch: removing them one at a time would emit a state event
// per job, so dragging across fifty rows would repaint the queue fifty times and show
// a half-emptied list in between. Unknown ids are skipped rather than reported -- the
// caller built the list from the rows it just clicked, and a row can vanish underneath
// it if the queue is running.
func (r *Runner) RemoveJobs(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return 0
	}

	r.mu.Lock()
	kept := make([]*Job, 0, len(r.jobs))
	removed := 0
	for _, j := range r.jobs {
		if !want[j.ID] {
			kept = append(kept, j)
			continue
		}
		if j.Status == StatusRunning || j.Status == StatusPreparing {
			if j.cancel != nil {
				j.cancel()
			}
		}
		delete(r.index, j.ID)
		removed++
	}
	r.jobs = kept
	r.mu.Unlock()
	if removed > 0 {
		r.emitState()
	}
	return removed
}

// ClearFinished removes every job that reached a terminal state.
// Returns how many were removed.
func (r *Runner) ClearFinished() int {
	r.mu.Lock()
	kept := make([]*Job, 0, len(r.jobs))
	removed := 0
	for _, j := range r.jobs {
		if j.Status.Finished() {
			delete(r.index, j.ID)
			removed++
			continue
		}
		kept = append(kept, j)
	}
	r.jobs = kept
	r.mu.Unlock()
	r.emitState()
	return removed
}

// ClearAll empties the queue.
func (r *Runner) ClearAll() int {
	r.mu.Lock()
	n := len(r.jobs)
	for _, j := range r.jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	r.jobs = nil
	r.index = map[string]*Job{}
	r.mu.Unlock()
	r.emitState()
	return n
}

// UpdateJobTemplate re-points a single job at another template.
func (r *Runner) UpdateJobTemplate(id, templateID, templateName string) error {
	r.mu.Lock()
	j := r.index[id]
	r.mu.Unlock()
	if j == nil {
		return fmt.Errorf("任务不存在")
	}
	j.lock()
	defer j.unlock()
	if j.Status == StatusRunning || j.Status == StatusPreparing {
		return fmt.Errorf("任务正在处理，无法切换模板")
	}
	j.TemplateID = templateID
	j.TemplateName = templateName
	if j.Status.Finished() {
		j.Status = StatusPending
		j.Progress = 0
		j.Message = "排队中"
		j.Error = ""
		j.Warnings = nil
		j.LogTail = nil
	}
	r.cond.Broadcast()
	return nil
}

// UpdateAllTemplates re-points every not-yet-finished job at another template.
func (r *Runner) UpdateAllTemplates(templateID, templateName string) int {
	r.mu.Lock()
	n := 0
	for _, j := range r.jobs {
		if j.Status.Finished() {
			continue
		}
		j.lock()
		j.TemplateID = templateID
		j.TemplateName = templateName
		j.unlock()
		n++
	}
	r.cond.Broadcast()
	r.mu.Unlock()
	if n > 0 {
		r.emitState()
	}
	return n
}

// ResetFailed re-queues every failed or cancelled job.
func (r *Runner) ResetFailed() int {
	r.mu.Lock()
	n := 0
	for _, j := range r.jobs {
		j.lock()
		if j.Status == StatusFailed || j.Status == StatusCanceled || j.Status == StatusSkipped {
			j.Status = StatusPending
			j.Message = "排队中"
			j.Progress = 0
			j.Error = ""
			j.Warnings = nil
			j.LogTail = nil
			j.LogLineCount = 0
			j.done = make(chan struct{})
			n++
		}
		j.unlock()
	}
	if n > 0 {
		r.paused = false
	}
	r.cond.Broadcast()
	r.mu.Unlock()
	r.emitState()
	return n
}

// ---------------------------------------------------------------------------
// Execution control
// ---------------------------------------------------------------------------

// Start ensures enough workers exist and un-pauses the queue.
func (r *Runner) Start() {
	r.mu.Lock()
	r.ensureWorkersLocked(r.prov.ConcurrencyOrDefault())
	if r.stopping {
		r.ctx, r.cancelAll = context.WithCancel(context.Background())
		r.stopping = false
	}
	r.paused = false
	r.cond.Broadcast()
	r.mu.Unlock()
	r.slots.restart()
	r.emitState()
}

// EnsureWorkers grows the pool to match n without starting the queue.
func (r *Runner) EnsureWorkers(n int) {
	r.mu.Lock()
	r.ensureWorkersLocked(n)
	r.mu.Unlock()
}

func (r *Runner) ensureWorkersLocked(n int) {
	if n < 1 {
		n = 1
	}
	for r.workers < n {
		r.workers++
		go r.worker()
	}
}

// Pause stops handing out new jobs; running jobs finish normally.
func (r *Runner) Pause() {
	r.mu.Lock()
	r.paused = true
	r.mu.Unlock()
	r.emitState()
}

// Resume continues a paused queue.
func (r *Runner) Resume() {
	r.mu.Lock()
	r.paused = false
	r.cond.Broadcast()
	r.mu.Unlock()
	r.emitState()
}

// TogglePause flips the pause state and reports the new value.
func (r *Runner) TogglePause() bool {
	r.mu.Lock()
	r.paused = !r.paused
	if !r.paused {
		r.cond.Broadcast()
	}
	v := r.paused
	r.mu.Unlock()
	r.emitState()
	return v
}

// CancelAll running jobs and re-queue the pending ones as cancelled.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	r.paused = true
	for _, j := range r.jobs {
		if j.cancel != nil {
			j.cancel()
		}
		if j.Status == StatusPending {
			j.Status = StatusCanceled
			j.Message = "已取消"
		}
	}
	r.cond.Broadcast()
	r.mu.Unlock()
	// Release anyone blocked on a per-template slot: their contexts are cancelled
	// above, but a waiting job has no cancel func yet, so the throttle is the only
	// way out of that wait.
	r.slots.stop()
	r.emitState()
}

// Stats reports queue counters.
func (r *Runner) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statsLocked()
}

func (r *Runner) statsLocked() Stats {
	s := Stats{Total: len(r.jobs), Paused: r.paused, Workers: r.workers}
	done := 0
	for _, j := range r.jobs {
		switch j.Status {
		case StatusPending:
			s.Pending++
		case StatusPreparing:
			s.Running++
		case StatusRunning:
			s.Running++
		case StatusDone:
			s.Done++
			done++
		case StatusWarning:
			s.Warning++
			done++
		case StatusFailed:
			s.Failed++
			done++
		case StatusCanceled:
			s.Canceled++
			done++
		case StatusSkipped:
			s.Skipped++
			done++
		case StatusFiltered:
			s.Filtered++
			done++
		}
	}
	if s.Total > 0 {
		s.Progress = float64(done) / float64(s.Total)
	}
	return s
}

func (r *Runner) emitState() {
	r.mu.Lock()
	st := r.statsLocked()
	emit := r.emit
	r.mu.Unlock()
	if emit != nil {
		emit(EventQueue, st)
	}
}

func (r *Runner) emitJob(j *Job) {
	r.mu.Lock()
	emit := r.emit
	r.mu.Unlock()
	if emit != nil {
		emit(EventJobUpdate, j.Snapshot())
	}
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

func (r *Runner) worker() {
	for {
		job := r.take()
		if job == nil {
			return
		}
		r.runJobSafe(job)
	}
}

func (r *Runner) take() *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		if r.stopping {
			return nil
		}
		if !r.paused {
			for _, j := range r.jobs {
				if j.Status == StatusPending {
					j.lock()
					j.Status = StatusPreparing
					j.Message = "分析中"
					j.StartedAt = time.Now()
					j.unlock()
					return j
				}
			}
		}
		r.cond.Wait()
	}
}

func (r *Runner) runJobSafe(job *Job) {
	defer func() {
		if rec := recover(); rec != nil {
			job.lock()
			job.Status = StatusFailed
			job.Error = fmt.Sprintf("内部错误: %v", rec)
			job.Message = job.Error
			job.EndedAt = time.Now()
			job.unlock()
			r.emitJob(job)
		}
		r.mu.Lock()
		r.cond.Broadcast()
		anyRunning := r.anyRunningLocked()
		r.mu.Unlock()

		if !anyRunning {
			// Nothing left to do: let the machine sleep again.
			r.keepAwake.Disable()
		}
		r.emitState()
	}()
	r.runJob(job)
}

func (r *Runner) anyRunningLocked() bool {
	for _, j := range r.jobs {
		if j.Status == StatusRunning || j.Status == StatusPreparing {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The pipeline for a single job
// ---------------------------------------------------------------------------

func (r *Runner) runJob(job *Job) {
	r.mu.Lock()
	prov := r.prov
	r.mu.Unlock()
	s := prov.Settings()
	bins := prov.Binaries()

	// Hold the machine awake for as long as a batch is in flight.
	if s.PreventSleep {
		r.keepAwake.Enable(false)
	}

	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()

	job.lock()
	job.cancel = cancel
	job.Error = ""
	job.Warnings = nil
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now()
	}
	job.unlock()

	stats := newStreamStats()

	fail := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		job.lock()
		job.Status = StatusFailed
		job.Error = msg
		job.Message = msg
		job.EndedAt = time.Now()
		job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		job.unlock()
		r.emitJob(job)
		r.record(job, s, StatusFailed, msg)
	}

	if !bins.Ready() {
		fail("未找到 ffmpeg / ffprobe，请在「设置 → 二进制与路径」中指定")
		return
	}

	tpl, ok := r.prov.Template(job.TemplateID)
	if !ok {
		fail("模板不存在或已被删除")
		return
	}

	// Everything the template did not override comes from the global template, so
	// the rest of this function can read one merged value instead of doing
	// per-field fallbacks. eff is read-only: it shares the global template's
	// section pointers.
	eff := tpl.Effective(prov.Global())
	perf := eff.Perf
	if perf == nil {
		perf = &store.PerfSpec{Concurrency: 1, LogLevel: "warning"}
	}
	// A template may ask for less parallelism than the ceiling but never more.
	jobConcurrency := perf.Concurrency
	if jobConcurrency < 1 {
		jobConcurrency = 1
	}
	if ceiling := prov.ConcurrencyOrDefault(); jobConcurrency > ceiling {
		jobConcurrency = ceiling
	}
	if !r.slots.acquire(ctx, job.TemplateID, jobConcurrency) {
		// The queue was stopped or this job was cancelled while it waited for a
		// free slot. Nothing has run yet, so just hand the job back.
		r.finishCanceled(job, s, perf.DeleteOnFail)
		return
	}
	defer r.slots.release(job.TemplateID, jobConcurrency)

	// --- 1. probe ---
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Minute)
	info, err := media.ProbeFile(probeCtx, bins.FFprobe, job.Input)
	probeCancel()
	if err != nil {
		if ctx.Err() != nil {
			r.finishCanceled(job, s, perf.DeleteOnFail)
			return
		}
		fail("%v", err)
		return
	}
	job.lock()
	job.InfoBefore = info
	job.Duration = info.Duration
	job.Size = info.Size
	job.Status = StatusRunning
	job.Message = "处理中"
	job.unlock()
	r.emitJob(job)

	// --- 2. filter rules ---
	// The queue-level override wins over whatever the template resolved to; it is the
	// user's "for this batch only" rule. Read once per job so a change mid-run does
	// not make two files in the same batch obey different rules.
	filter := eff.Filter
	if qf := r.queueFilterSpec(); qf != nil {
		cp := *qf
		filter = &cp
	}
	if filter == nil {
		filter = &store.FilterSpec{}
	}
	if pass, reason := EvaluateFilters(info, *filter); !pass {
		// The status is set only after the transfer. Publishing it first would let
		// the queue look finished -- pending and running both zero -- while the
		// file was still being moved, so the UI would report a relocation that had
		// not happened yet.
		var mErr error
		var dest string
		if filter.HandlesExcluded() {
			dest, mErr = Relocate(MoveRequest{
				Src:       job.Input,
				SrcRoot:   job.SourceRoot,
				Dest:      filter.Dest,
				Fallback:  store.DestRule{Mode: store.OutputCustom, Suffix: store.DefaultOutputSuffix},
				Pattern:   filter.RenamePattern,
				Overwrite: filter.Overwrite,
				Copy:      filter.Action == store.ActionCopy,
				Template:  tpl.Name,
			})
		}

		verb := "移动"
		if filter.Action == store.ActionCopy {
			verb = "复制"
		}
		job.lock()
		job.Status = StatusFiltered
		job.Message = reason
		job.EndedAt = time.Now()
		job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		if filter.HandlesExcluded() {
			if mErr != nil {
				job.Error = mErr.Error()
				job.Message = reason + "；" + verb + "失败: " + mErr.Error()
			} else {
				job.Output = dest
				job.OutputName = filepath.Base(dest)
				job.Message = reason + "；已" + verb + "到 " + dest
			}
		}
		job.unlock()

		if filter.HandlesExcluded() {
			r.log(job, s, fmt.Sprintf("[filter] %s", job.Message))
		} else {
			r.log(job, s, fmt.Sprintf("[filter] 已排除: %s", reason))
		}
		r.emitJob(job)
		r.record(job, s, StatusFiltered, job.Message)
		return
	}

	// --- 3. resolve output ---
	out, err := ResolveOutput(OutputRequest{
		Info: info, Tpl: eff, SrcRoot: job.SourceRoot,
	})
	if err != nil {
		fail("%v", err)
		return
	}
	if eff.OutConflict == store.ConflictSkip {
		if st, err := os.Stat(out); err == nil && st.Size() > 0 {
			job.lock()
			job.Output = out
			job.OutputName = filepath.Base(out)
			job.Status = StatusSkipped
			job.Message = "输出文件已存在，已跳过"
			job.EndedAt = time.Now()
			job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
			job.unlock()
			r.emitJob(job)
			r.record(job, s, StatusSkipped, "输出文件已存在")
			return
		}
	}

	// --- 4. plan ---
	plan, err := BuildPlan(PlanInput{
		Info: info, Tpl: eff, Settings: s, Binaries: bins, Output: out,
		LogLevel: perf.LogLevel, Threads: perf.Threads,
	})
	if err != nil {
		fail("%v", err)
		return
	}
	job.lock()
	job.Output = out
	job.OutputName = filepath.Base(out)
	job.Command = plan.Command
	job.TargetWidth = plan.TargetW
	job.TargetHeight = plan.TargetH
	job.Resized = plan.Resized
	for _, w := range plan.Warnings {
		job.addWarningLocked(w)
	}
	job.unlock()
	r.emitJob(job)

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fail("无法创建输出目录: %v", err)
		return
	}
	r.log(job, s, "$ "+plan.Command)
	for _, w := range plan.Warnings {
		r.log(job, s, "[warn] "+w)
	}

	// --- 5. run (with retries) ---
	attempts := perf.RetryCount + 1
	var runErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			r.finishCanceled(job, s, perf.DeleteOnFail)
			return
		}
		runErr = r.execFFmpeg(ctx, job, plan, bins, s, stats)
		if runErr == nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
		if attempt < attempts {
			r.log(job, s, fmt.Sprintf("[retry] 第 %d 次失败，正在重试…", attempt))
			time.Sleep(1200 * time.Millisecond)
		}
	}

	if ctx.Err() != nil {
		r.finishCanceled(job, s, perf.DeleteOnFail)
		return
	}

	if runErr != nil {
		if perf.DeleteOnFail {
			_ = os.Remove(out)
		}
		fail("%v", runErr)
		r.handleProblemFile(job, s, eff, StatusFailed)
		return
	}

	// --- 6. verify + probe output ---
	st, statErr := os.Stat(out)
	if statErr != nil || st.Size() == 0 {
		if perf.DeleteOnFail {
			_ = os.Remove(out)
		}
		fail("ffmpeg 已退出但输出文件缺失或为空")
		return
	}

	outCtx, outCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	outInfo, outErr := media.ProbeFile(outCtx, bins.FFprobe, out)
	outCancel()

	job.lock()
	job.Progress = 1
	if outErr == nil {
		job.InfoAfter = outInfo
	} else {
		job.addWarningLocked("输出文件复核失败: " + outErr.Error())
	}
	job.EndedAt = time.Now()
	job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	status := StatusDone
	if len(job.Warnings) > 0 {
		status = StatusWarning
	}
	job.Status = status
	if status == StatusDone {
		job.Message = "已完成"
	} else {
		job.Message = fmt.Sprintf("已完成（%d 条警告）", len(job.Warnings))
	}
	job.unlock()

	r.log(job, s, fmt.Sprintf("[done] 输出 %s，耗时 %s", HumanSize(st.Size()), HumanDuration(float64(job.Snapshot().ElapsedMS)/1000)))
	r.emitJob(job)
	r.record(job, s, status, job.Snapshot().Message)

	if status == StatusWarning {
		r.handleProblemFile(job, s, eff, StatusWarning)
	}
}

// finishCanceled marks the job as cancelled. deleteOnFail comes from the merged
// template so a per-template "delete partial output" overrides the global one.
func (r *Runner) finishCanceled(job *Job, s store.Settings, deleteOnFail bool) {
	job.lock()
	job.Status = StatusCanceled
	job.Message = "已取消"
	job.EndedAt = time.Now()
	job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	out := job.Output
	job.unlock()
	if deleteOnFail && out != "" {
		_ = os.Remove(out)
	}
	r.log(job, s, "[cancel] 任务已取消")
	r.emitJob(job)
	r.record(job, s, StatusCanceled, "用户取消")
}

// handleProblemFile applies the error / warning policies to the source file.
// tpl must be the merged template: the problem section is nil whenever the
// template follows the global one.
func (r *Runner) handleProblemFile(job *Job, s store.Settings, tpl store.Template, status Status) {
	spec := tpl.Problems
	if spec == nil {
		return
	}
	statusKey := store.StatusWarning
	if status != StatusWarning {
		statusKey = "error"
	}
	action := spec.Action(statusKey)
	if action != store.ActionMove && action != store.ActionCopy {
		return
	}
	rule := spec.Dest(statusKey)
	if !rule.Usable() {
		return
	}

	src := job.Input
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		// The source may already have been moved by the filter stage.
		if job.Snapshot().InfoBefore != nil {
			src = job.Snapshot().InfoBefore.Path
		}
		if _, err := os.Stat(src); err != nil {
			return
		}
	}

	dest, err := Relocate(MoveRequest{
		Src:       src,
		SrcRoot:   job.SourceRoot,
		Dest:      rule,
		Fallback:  store.DestRule{Mode: store.OutputMirror, Suffix: store.DefaultOutputSuffix},
		Pattern:   "{name}.{ext}",
		Overwrite: false,
		Copy:      action == store.ActionCopy,
		Template:  tpl.Name,
	})
	if err != nil {
		r.log(job, s, "[policy] 文件处理失败: "+err.Error())
		job.lock()
		job.addWarningLocked("按策略处理源文件失败: " + err.Error())
		job.unlock()
		r.emitJob(job)
		return
	}
	verb := "移动"
	if action == store.ActionCopy {
		verb = "复制"
	}
	r.log(job, s, fmt.Sprintf("[policy] 源文件已%s到 %s", verb, dest))
	job.lock()
	job.addWarningLocked(fmt.Sprintf("源文件已%s到 %s", verb, dest))
	job.unlock()
	r.emitJob(job)
}

// ---------------------------------------------------------------------------
// ffmpeg process
// ---------------------------------------------------------------------------

func (r *Runner) execFFmpeg(ctx context.Context, job *Job, plan *Plan, bins media.Binaries, s store.Settings, stats *streamStats) error {
	cmd := exec.CommandContext(ctx, bins.FFmpeg, plan.Args...)
	cmd.SysProcAttr = sysx.NoWindow()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("无法读取进程输出: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("无法读取进程输出: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动 ffmpeg: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	buf := newLogBuffer(job, r, s)

	go func() {
		defer wg.Done()
		scanLines(stdout, func(line string) {
			r.handleProgressLine(job, line, stats, buf)
		})
	}()
	go func() {
		defer wg.Done()
		scanLines(stderr, func(line string) {
			r.handleStderrLine(job, line, stats, buf)
		})
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	buf.Flush(true)

	job.lock()
	job.Speed = stats.avgSpeed()
	// Surface non-fatal complaints collected from stderr.
	for _, w := range stats.newWarnings() {
		job.addWarningLocked(w)
	}
	job.unlock()
	r.emitJob(job)

	// The exit code is the only reliable failure signal: ffmpeg happily prints
	// the word "error" for recoverable decode glitches while still producing a
	// perfectly good file.
	if waitErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := stats.lastError()
		if msg == "" {
			msg = stats.lastLines(2)
		}
		if msg == "" {
			msg = waitErr.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func scanLines(rc io.Reader, fn func(string)) {
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		fn(strings.TrimRight(sc.Text(), "\r"))
	}
}

func (r *Runner) handleProgressLine(job *Job, line string, stats *streamStats, buf *logBuffer) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	switch k {
	case "frame":
		stats.setFrame(parseInt64(v))
	case "fps":
		stats.setFPS(parseFloat(v))
	case "bitrate":
		stats.setBitrate(strings.TrimSuffix(v, "kbits/s"))
	case "total_size":
		stats.setSize(parseInt64(v))
	case "out_time_us", "out_time_ms":
		// ffmpeg reports both keys in microseconds.
		stats.setOutTimeUS(parseInt64(v))
	case "out_time":
		stats.setOutTimeString(v)
	case "speed":
		stats.setSpeed(strings.TrimSuffix(v, "x"))
	case "progress":
		r.applyProgress(job, stats)
		buf.Tick()
		if v == "end" {
			stats.markEnded()
		}
	}
}

func (r *Runner) applyProgress(job *Job, stats *streamStats) {
	snap := stats.snapshot()

	job.lock()
	job.Frame = snap.frame
	job.FPS = snap.fps
	job.Bitrate = snap.bitrate
	job.OutBytes = snap.size
	job.OutTimeMS = snap.outTimeMS
	job.Speed = snap.speed
	dur := job.Duration
	if dur <= 0 {
		if job.InfoBefore != nil {
			dur = job.InfoBefore.Duration
		}
	}
	if dur <= 0 {
		dur = stats.duration()
	}
	if dur > 0 {
		p := float64(snap.outTimeMS) / 1000 / dur
		if p < 0 {
			p = 0
		}
		if p > 0.999 {
			p = 0.999
		}
		job.Progress = p
		job.Message = fmt.Sprintf("%.1f%% · %s · %sx",
			p*100, HumanDuration(float64(snap.outTimeMS)/1000), trimNum(snap.speed))
	} else {
		job.Message = fmt.Sprintf("已处理 %s · %sx",
			HumanDuration(float64(snap.outTimeMS)/1000), trimNum(snap.speed))
	}
	job.unlock()
	r.emitJob(job)
}

func (r *Runner) handleStderrLine(job *Job, line string, stats *streamStats, buf *logBuffer) {
	line = strings.TrimRight(line, " ")
	if strings.TrimSpace(line) == "" {
		return
	}
	stats.observe(line)
	buf.Add(line)

	low := strings.ToLower(line)
	if strings.HasPrefix(low, "frame=") {
		// Traditional stats line (appears when -nostats is not honoured).
		return
	}
	switch {
	case strings.Contains(low, "error"), strings.Contains(low, "invalid"),
		strings.Contains(low, "failed"), strings.Contains(low, "no such file"):
		stats.addError(line)
	case strings.Contains(low, "warning"), strings.Contains(low, "deprecated"),
		strings.Contains(low, "not supported"), strings.Contains(low, "unknown encoder"):
		stats.addWarning(line)
	}
}

func (r *Runner) log(job *Job, s store.Settings, line string) {
	job.lock()
	job.appendLogLocked(line, s.KeepLogLines)
	job.unlock()
	r.mu.Lock()
	emit := r.emit
	r.mu.Unlock()
	if emit != nil {
		emit(EventJobLog, LogBatch{JobID: job.ID, Lines: []string{line}})
	}
}

// ---------------------------------------------------------------------------
// log batching
// ---------------------------------------------------------------------------

// logBuffer coalesces stderr lines so the frontend is not flooded by events.
type logBuffer struct {
	job  *Job
	r    *Runner
	s    store.Settings
	mu   sync.Mutex
	pend []string
	last time.Time
}

func newLogBuffer(job *Job, r *Runner, s store.Settings) *logBuffer {
	return &logBuffer{job: job, r: r, s: s, last: time.Now()}
}

func (b *logBuffer) Add(line string) {
	b.mu.Lock()
	b.pend = append(b.pend, line)
	b.job.lock()
	b.job.appendLogLocked(line, b.s.KeepLogLines)
	b.job.unlock()
	flush := len(b.pend) >= 40 || time.Since(b.last) > 250*time.Millisecond
	b.mu.Unlock()
	if flush {
		b.Flush(false)
	}
}

// Tick flushes if enough time passed; called on every progress block.
func (b *logBuffer) Tick() {
	b.mu.Lock()
	due := len(b.pend) > 0 && time.Since(b.last) > 250*time.Millisecond
	b.mu.Unlock()
	if due {
		b.Flush(false)
	}
}

// Flush pushes buffered lines to the frontend.
func (b *logBuffer) Flush(force bool) {
	b.mu.Lock()
	if len(b.pend) == 0 {
		b.mu.Unlock()
		return
	}
	if !force && time.Since(b.last) < 120*time.Millisecond {
		b.mu.Unlock()
		return
	}
	lines := b.pend
	b.pend = nil
	b.last = time.Now()
	b.mu.Unlock()

	b.r.mu.Lock()
	emit := b.r.emit
	b.r.mu.Unlock()
	if emit != nil {
		emit(EventJobLog, LogBatch{JobID: b.job.ID, Lines: lines})
	}
}

// ---------------------------------------------------------------------------
// stream stats
// ---------------------------------------------------------------------------

type streamStats struct {
	mu         sync.Mutex
	frame      int64
	fps        float64
	bitrate    string
	size       int64
	outTimeUS  int64
	speed      float64
	speedSum   float64
	speedCount int
	lastSpeed  float64
	dur        float64
	errs       []string
	warns      []string
	warned     map[string]bool
	tail       []string
	ended      bool
}

type statsSnapshot struct {
	frame     int64
	fps       float64
	bitrate   string
	size      int64
	outTimeMS int64
	speed     float64
}

func newStreamStats() *streamStats { return &streamStats{warned: map[string]bool{}} }

func (s *streamStats) setFrame(v int64) { s.mu.Lock(); s.frame = v; s.mu.Unlock() }
func (s *streamStats) setFPS(v float64) { s.mu.Lock(); s.fps = v; s.mu.Unlock() }
func (s *streamStats) setSize(v int64)  { s.mu.Lock(); s.size = v; s.mu.Unlock() }
func (s *streamStats) setBitrate(v string) {
	v = strings.TrimSpace(v)
	s.mu.Lock()
	if v != "" && v != "N/A" {
		s.bitrate = v
	}
	s.mu.Unlock()
}

func (s *streamStats) setOutTimeUS(v int64) {
	s.mu.Lock()
	if v > s.outTimeUS {
		s.outTimeUS = v
	}
	s.mu.Unlock()
}

func (s *streamStats) setOutTimeString(v string) {
	sec := parseTimestamp(v)
	if sec <= 0 {
		return
	}
	s.mu.Lock()
	us := int64(sec * 1e6)
	if us > s.outTimeUS {
		s.outTimeUS = us
	}
	s.mu.Unlock()
}

func (s *streamStats) setSpeed(v string) {
	v = strings.TrimSpace(v)
	if v == "" || v == "N/A" {
		return
	}
	f := parseFloat(v)
	if f <= 0 {
		return
	}
	s.mu.Lock()
	s.lastSpeed = f
	s.speedSum += f
	s.speedCount++
	s.mu.Unlock()
}

func (s *streamStats) markEnded() { s.mu.Lock(); s.ended = true; s.mu.Unlock() }

func (s *streamStats) snapshot() statsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statsSnapshot{
		frame:     s.frame,
		fps:       s.fps,
		bitrate:   s.bitrate,
		size:      s.size,
		outTimeMS: s.outTimeUS / 1000,
		speed:     s.lastSpeed,
	}
}

func (s *streamStats) avgSpeed() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.speedCount == 0 {
		return 0
	}
	return s.speedSum / float64(s.speedCount)
}

func (s *streamStats) duration() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dur
}

func (s *streamStats) failed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.errs) > 0
}

func (s *streamStats) addError(line string) {
	s.mu.Lock()
	if len(s.errs) < 8 {
		s.errs = append(s.errs, line)
	}
	s.mu.Unlock()
}

func (s *streamStats) addWarning(line string) {
	s.mu.Lock()
	if len(s.warns) < 30 {
		s.warns = append(s.warns, line)
	}
	s.mu.Unlock()
}

// newWarnings returns only the warnings that were not reported to the job yet.
func (s *streamStats) newWarnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.warns))
	for _, w := range s.warns {
		if s.warned[w] {
			continue
		}
		s.warned[w] = true
		out = append(out, w)
	}
	return out
}

// lastLines returns the trailing stderr lines, used as a fallback error message.
func (s *streamStats) lastLines(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tail) == 0 {
		return ""
	}
	from := len(s.tail) - n
	if from < 0 {
		from = 0
	}
	return strings.Join(s.tail[from:], " / ")
}

func (s *streamStats) lastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) == 0 {
		return ""
	}
	// The last line usually carries the actual cause.
	return strings.TrimSpace(s.errs[len(s.errs)-1])
}

func (s *streamStats) warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.warns...)
}

// observe records a stderr line: keeps a short tail for error reporting and
// picks up the input duration ffmpeg announces on startup.
func (s *streamStats) observe(line string) {
	s.mu.Lock()
	s.tail = append(s.tail, line)
	if len(s.tail) > 12 {
		s.tail = append([]string(nil), s.tail[len(s.tail)-12:]...)
	}
	s.mu.Unlock()

	if !strings.Contains(line, "Duration:") {
		return
	}
	i := strings.Index(line, "Duration:")
	if i < 0 {
		return
	}
	rest := strings.TrimSpace(line[i+len("Duration:"):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return
	}
	if sec := parseTimestamp(strings.TrimSuffix(fields[0], ",")); sec > 0 {
		s.mu.Lock()
		if s.dur <= 0 {
			s.dur = sec
		}
		s.mu.Unlock()
	}
}

func parseTimestamp(v string) float64 {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 3 {
		return 0
	}
	h := parseFloat(parts[0])
	m := parseFloat(parts[1])
	sec := parseFloat(parts[2])
	if h < 0 || m < 0 || sec < 0 {
		return 0
	}
	return h*3600 + m*60 + sec
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// ---------------------------------------------------------------------------
// History records
// ---------------------------------------------------------------------------

func summaryOf(info *media.Info) store.MediaSummary {
	if info == nil {
		return store.MediaSummary{}
	}
	s := store.MediaSummary{
		Exists:    true,
		Path:      info.Path,
		Size:      info.Size,
		Container: firstToken(info.Container),
		Duration:  info.Duration,
		Width:     info.DisplayWidth,
		Height:    info.DisplayHeight,
		FPS:       info.FPS,
		PixFmt:    info.PixFmt,
		BitRate:   info.BitRate,
	}
	if info.Video != nil {
		s.VideoCodec = info.Video.Codec
	}
	if info.Audio != nil {
		s.AudioCodec = info.Audio.Codec
		s.SampleRate = info.Audio.SampleRate
		s.Channels = info.Audio.Channels
	}
	return s
}

func firstToken(s string) string {
	if i := strings.IndexByte(s, ','); i > 0 {
		return s[:i]
	}
	return s
}

// record builds and hands a history entry to the application layer.
func (r *Runner) record(job *Job, s store.Settings, status Status, note string) {
	snap := job.Snapshot()

	rec := store.Record{
		ID:           newJobID(),
		Input:        snap.Input,
		Output:       snap.Output,
		TemplateID:   snap.TemplateID,
		TemplateName: snap.TemplateName,
		Command:      snap.Command,
		Status:       status.Label(),
		Note:         note,
		Error:        snap.Error,
		Warnings:     snap.Warnings,
		StartedAt:    snap.StartedAt,
		EndedAt:      snap.EndedAt,
		ElapsedMS:    snap.ElapsedMS,
		Speed:        snap.Speed,
		Before:       summaryOf(snap.InfoBefore),
		After:        summaryOf(snap.InfoAfter),
	}
	if status == StatusFiltered || status == StatusSkipped {
		rec.After = store.MediaSummary{}
	}
	if snap.Output != "" && status != StatusFiltered {
		if st, err := os.Stat(snap.Output); err == nil && rec.After.Size == 0 {
			rec.After.Exists = true
			rec.After.Path = snap.Output
			rec.After.Size = st.Size()
		}
	}
	if rec.EndedAt.IsZero() {
		rec.EndedAt = time.Now()
	}

	// Persist the full log for this task when the user asked for it. The write
	// also prunes the folder against the age and size limits, so the cost is
	// amortised over the tasks themselves rather than a separate timer.
	if s.SaveRunLog && len(snap.LogTail) > 0 {
		maxBytes := int64(s.LogMaxSizeMB) * 1024 * 1024
		_, _ = store.WriteRunLog(s.LogDir, snap.StartedAt, snap.InputName,
			snap.LogTail, maxBytes, s.LogKeepDays)
	}

	r.mu.Lock()
	job.RecordID = rec.ID
	cb := r.onRecord
	emit := r.emit
	r.mu.Unlock()

	if cb != nil {
		cb(rec)
	}
	if emit != nil {
		emit(EventRecord, rec)
	}
}

// SortedIDs is a small helper used by tests and diagnostics.
func (r *Runner) SortedIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.index))
	for id := range r.index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
