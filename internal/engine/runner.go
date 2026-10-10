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

	// armed is "the user pressed 开始". Workers exist as soon as a template is
	// loaded so the pool is warm, but they must not touch a job until then --
	// otherwise dropping a file onto the window starts encoding it, and there is
	// no way back short of pressing 停止. paused is a different thing entirely:
	// the queue was started and is being held back on purpose.
	armed     bool
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

	// processedThisRun is the 「已处理过的文件」 record: "this source has already
	// been through this template, successfully, since the app started".
	//
	// It lives in memory and nowhere else, on purpose. A record on disk outlives
	// the reason for it -- a folder that was re-organised, a result that was
	// moved by hand -- and then it silently skips files the user did ask to
	// process. Keeping it in the process means the promise is exactly one a user
	// can verify: same session, already done, skipped; new session, everything
	// runs again. Nothing is written next to anyone's media files.
	//
	// Guarded by mu, keyed by processedKey.
	processedThisRun map[string]bool
}

// NewRunner creates an idle runner.
func NewRunner() *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		index:            map[string]*Job{},
		processedThisRun: map[string]bool{},
		keepAwake:        sysx.NewKeepAwake(),
		ctx:              ctx,
		cancelAll:        cancel,
		slots:            newThrottle(),
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

// Providers returns the configured providers, or nil before Configure.
func (r *Runner) Providers() *Providers {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prov.Settings == nil && r.prov.Template == nil {
		return nil
	}
	p := r.prov
	return &p
}

// Shutdown stops every worker and releases the sleep inhibitor.
func (r *Runner) Shutdown() {
	r.slots.stop()
	r.mu.Lock()
	r.stopping = true
	r.cancelAll()
	r.cond.Broadcast()
	r.mu.Unlock()
	// Frozen processes first: cancelling their context only closes the pipes, and
	// a suspended ffmpeg cannot act on that. Without this the user quits the app
	// and finds an orphan ffmpeg.exe still holding the output file.
	r.killSuspended()
	r.keepAwake.Disable()
}

// ---------------------------------------------------------------------------
// Queue management
// ---------------------------------------------------------------------------

// InputItem is a file or directory the user dropped into the queue.
type InputItem struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	// Recursive is 收多深，而且它已经是**算完的**结果：`store.DirFilter.Recursive`
	// 把"设置说只看这一层"和"调用方说不递归"两个来源收窄到一起，这里不再看 `Dirs`。
	Recursive bool `json:"recursive"`
	// Dirs 是目录规则：这个目录里收哪几个子目录、是收还是排、命中几条才算。
	//
	// 它挂在这一项上而不是让 AddInputs 自己去读设置：设置是 App 的事，runner 只管
	// "把给我的东西变成任务"。于是拖入、点选文件夹、添加文件三条路必然过同一段代码
	// —— 有一条自己另做一遍过滤，两边的差就只能在"拖了才发现"的时候暴露。
	Dirs store.NameRules `json:"dirs"`
	// Files 是文件规则：收进来的文件里再按**文件名**筛一遍。它和 `Dirs` 完全独立
	// —— 目录全收、文件只要其中几个是最常见的用法之一。
	Files store.NameRules `json:"files"`
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
			// A file named outright used to skip the extension check that a
			// scanned directory applies, so dragging a .zip onto the window put
			// a job in the queue that ffmpeg could never open: it counted
			// towards the totals but had nothing to show. Same gate, same
			// message, whether the file was walked or named.
			if !isMediaFile(p) {
				errs = append(errs, fmt.Sprintf("%s: 不是媒体文件，已跳过", filepath.Base(p)))
				continue
			}
			// 点名的文件也要过文件规则。它和扫出来的文件是同一件事——"这个文件
			// 进不进队列"——所以规则只有一套：拖进来一个文件时跳过这一步，会让
			// "文件过滤"只在扫目录时才有效，而用户看不出这两种情况有什么不同。
			if !it.Files.Take(filepath.Base(p)) {
				errs = append(errs, fmt.Sprintf("%s: 被文件过滤条件排除", filepath.Base(p)))
				continue
			}
			added = append(added, NewJob(p, filepath.Dir(p), templateID, templateName))
			continue
		}

		// 目录：先按条件挑出要收的那几个子目录，然后每一个都当一次"用户添加的
		// 目录"（`NewJob` 的 sourceRoot）—— 「同级目录」量的是它，所以产物落在
		// 它所在的位置旁边，而不是整棵树的根旁边。条件为空时 roots 就是它自己。
		//
		// 排除方向下 roots 也是它自己（见 ExpandFolder）："整个文件夹减去几个"
		// 里那个"整个文件夹"就是落点，条件在收集文件时把命中的子树跳掉。
		scan := FolderScan{
			Dir: p, Dirs: it.Dirs, Files: it.Files, Recursive: it.Recursive,
		}
		roots := []string{p}
		if scan.Filtering() && !scan.Dirs.Exclude {
			matched, _, ferrs := ExpandFolder(scan)
			errs = append(errs, ferrs...)
			if len(matched) == 0 {
				// 悄悄一个都不加是最难查的一种：文件夹明明"加进去了"，列表却是
				// 空的，而界面上没有任何地方提到过滤条件还在拦着。
				errs = append(errs, fmt.Sprintf("%s: 没有子目录符合过滤条件", filepath.Base(p)))
				continue
			}
			roots = matched
		}

		// 目录规则只在排除方向下参与"收哪些文件"；收的方向里 roots 已经是被挑中的
		// 目录，再拿条件筛一遍会把它们自己的子目录又筛一次。
		var dirExcl store.NameRules
		if it.Dirs.Exclude {
			dirExcl = it.Dirs
		}
		before := len(added)
		dropped := 0
		for _, root := range roots {
			w := mediaUnder(root, it.Recursive, dirExcl, it.Files)
			errs = append(errs, w.Errs...)
			dropped += w.Filtered
			for _, f := range w.Files {
				added = append(added, NewJob(f, root, templateID, templateName))
			}
		}
		// "一个都没收着"有两种原因，说清是哪一种。排除方向没有"没找到子目录"这一步
		// —— roots 永远是那个文件夹本身 —— 所以那句话在这里说。
		switch {
		case len(added) > before:
		case scan.Dirs.Exclude:
			errs = append(errs, fmt.Sprintf("%s: 全部文件都被过滤条件排除了", filepath.Base(p)))
		case dropped > 0:
			errs = append(errs, fmt.Sprintf("%s: %d 个文件被文件过滤条件排除", filepath.Base(p), dropped))
		}
	}

	if len(added) == 0 {
		return 0, errs
	}
	return r.appendJobs(added, errs)
}

// appendJobs puts the freshly built jobs at the end of the queue, skipping the
// inputs that are already there.
//
// 两处加入路径（拖入 / 扫描）共用它：去重、编号、广播各只有一份，否则"同一个文件
// 加两次会怎样"就会有两种答案。
func (r *Runner) appendJobs(added []*Job, errs []string) (int, []string) {
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
		// 1-based position across the whole queue, so {index} keeps counting up
		// across several adds and stays put when a job is removed. The queue
		// preview reads the same field, which is what keeps the two commands
		// identical instead of merely similar.
		j.Index = len(r.jobs) + 1
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

// UpdateAllTemplates re-points the whole queue at another template.
//
// The toolbar binds the queue to one template, so switching it has to move every
// row -- including the ones that already ran. Those go back to 排队中 (the same
// rule UpdateJobTemplate applies to a single job), otherwise a finished result
// would sit there built from parameters that are no longer anywhere on screen.
// Jobs already on the CPU keep the template they started with: re-pointing them
// mid-flight would only produce a half-old, half-new output file.
func (r *Runner) UpdateAllTemplates(templateID, templateName string) ApplyResult {
	var res ApplyResult
	r.mu.Lock()
	for _, j := range r.jobs {
		if j.Status == StatusRunning || j.Status == StatusPreparing {
			continue
		}
		j.lock()
		j.TemplateID = templateID
		j.TemplateName = templateName
		if j.Status.Finished() {
			j.Status = StatusPending
			j.Progress = 0
			j.Message = "排队中"
			j.Error = ""
			j.Warnings = nil
			j.LogTail = nil
			res.Requeued++
		}
		j.unlock()
		res.Applied++
	}
	r.cond.Broadcast()
	r.mu.Unlock()
	if res.Applied > 0 {
		r.emitState()
	}
	return res
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

// Start begins processing: it arms the queue and releases any pause. Pressing it
// is the only thing that does -- adding files no longer starts work by itself.
func (r *Runner) Start() {
	r.mu.Lock()
	r.ensureWorkersLocked(r.prov.ConcurrencyOrDefault())
	if r.stopping {
		r.ctx, r.cancelAll = context.WithCancel(context.Background())
		r.stopping = false
	}
	r.armed = true
	r.paused = false
	r.cond.Broadcast()
	r.mu.Unlock()
	r.slots.restart()
	r.emitState()
}

// Disarm stops handing out jobs without touching the ones already running. It is
// what 停止 uses to put the queue back in its initial state: a later Start then
// means "go again" rather than resuming something the user never began.
func (r *Runner) Disarm() {
	r.mu.Lock()
	r.armed = false
	r.mu.Unlock()
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

// Pause stops handing out new jobs AND freezes the ones already running.
//
// The freeze is the whole point: ffmpeg has no pause verb, so the only way to make
// 暂停 mean "this file stops here and picks up from here" is to suspend the process
// itself. Verified against a 1080p30 encode -- out_time_ms resumed 14s -> 22s and
// the final file matched a control run exactly in size, so nothing is re-encoded
// and nothing is lost.
//
// A process that cannot be frozen (it just exited, or Windows refused) is not an
// error: it finishes on its own, which is exactly the old behaviour, so the user
// gets a working pause either way. The failure is written to that job's log rather
// than swallowed.
func (r *Runner) Pause() {
	r.mu.Lock()
	r.paused = true
	jobs := append([]*Job(nil), r.jobs...)
	r.mu.Unlock()

	for _, j := range jobs {
		j.lock()
		pid, already := j.pid, j.suspend != nil
		j.unlock()
		if pid <= 0 || already {
			// pid <= 0 means still probing or already finished; an OS call on that
			// pid would either fail or hit whatever process recycled the number.
			//
			// already means this job holds a handle. Pause is a button, not a
			// toggle, and it can be pressed twice: suspending again would push the
			// kernel's suspend count to 2 and the single Resume in 继续 would leave
			// the process frozen forever.
			continue
		}
		s, err := sysx.Suspend(pid)
		if err != nil {
			r.logLine(j, fmt.Sprintf("[pause] 无法挂起进程 %d: %v", pid, err))
			continue
		}
		r.attachSuspend(j, pid, s)
	}
	r.emitState()
}

// Resume thaws every frozen process and lets the queue hand out work again.
func (r *Runner) Resume() {
	r.mu.Lock()
	r.paused = false
	r.cond.Broadcast()
	r.mu.Unlock()
	r.thawAll()
	r.emitState()
}

// TogglePause flips the pause state and reports the new value.
func (r *Runner) TogglePause() bool {
	if r.Stats().Paused {
		r.Resume()
		return false
	}
	r.Pause()
	return true
}

// runningJobs returns the jobs whose ffmpeg process is currently up.
//
// The list is copied under the runner mutex and then walked job by job under each
// job's own lock, so no lock is ever held across an OS call and the two mutexes
// are never nested.
func (r *Runner) runningJobs() []*Job {
	r.mu.Lock()
	refs := append([]*Job(nil), r.jobs...)
	r.mu.Unlock()

	var out []*Job
	for _, j := range refs {
		j.lock()
		pid := j.pid
		j.unlock()
		if pid > 0 {
			out = append(out, j)
		}
	}
	return out
}

// attachSuspend records the handle that will thaw the frozen process again, and
// marks the job so the UI can say 「已暂停」.
//
// It takes the job rather than a pid to look up. Windows recycles pids, so a
// finished ffmpeg's number can already belong to the next queue item's ffmpeg by
// the time we get here -- searching by pid would then mark the wrong row frozen
// and 继续 would thaw a process nobody had suspended.
//
// The OS call that froze the process deliberately happened before this: holding the
// job lock across OpenProcess would serialise every other writer on that job --
// including the progress reader -- behind a syscall.
func (r *Runner) attachSuspend(j *Job, pid int, s *sysx.SuspendedProcess) {
	j.lock()
	if j.pid != pid {
		// The process we froze has already been replaced -- ffmpeg exited between
		// the syscall and here, and a retry or the next queue item has taken over
		// this job. Attaching the handle now would freeze the badge onto work that
		// is not running, and 继续 would thaw a process nobody suspended.
		//
		// Thaw it rather than just dropping the handle: it is a real frozen
		// process, and nothing else here is going to wake it.
		j.unlock()
		_ = s.Resume()
		return
	}
	j.suspend = s
	j.Frozen = true
	j.unlock()
	r.emitJob(j)
}

// thawJob resumes one frozen job and clears its flag. It reports whether the job
// had been frozen in the first place.
func (r *Runner) thawJob(j *Job) bool {
	j.lock()
	s := j.suspend
	if s == nil {
		j.unlock()
		return false
	}
	j.suspend = nil
	j.Frozen = false
	j.unlock()

	// Thaw first, then clear the flag -- a failure leaves the process frozen, and
	// the flag is what lets the UI say so instead of showing a stalled bar as if
	// the job were still running.
	if err := s.Resume(); err != nil {
		r.markFrozen(j)
		r.logLine(j, "[pause] 恢复进程失败: "+err.Error())
		return true
	}
	r.emitJob(j)
	return true
}

// markFrozen re-flags a job whose process could not be thawed, so the UI stops
// claiming it is running.
func (r *Runner) markFrozen(j *Job) {
	j.lock()
	j.Frozen = true
	j.Message = "已暂停（恢复失败）"
	j.unlock()
	r.emitJob(j)
}

// thawAll resumes every frozen job and reports how many were thawing.
func (r *Runner) thawAll() int {
	n := 0
	for _, j := range r.runningJobs() {
		if r.thawJob(j) {
			n++
		}
	}
	return n
}

// killSuspended terminates every frozen process and drops its handle.
//
// A suspended ffmpeg has no runnable thread, so it will never notice the queue was
// cancelled or the app was closing: it would sit there holding the output file and
// burning a core indefinitely. Every path that stops work goes through here.
func (r *Runner) killSuspended() int {
	n := 0
	for _, j := range r.runningJobs() {
		j.lock()
		s := j.suspend
		j.suspend = nil
		j.Frozen = false
		j.unlock()
		if s != nil {
			s.Kill()
			n++
		}
	}
	return n
}

// logLine appends a runner-level note to one job. Used for the failure paths of the
// freeze/thaw primitives, which would otherwise only reach the console.
func (r *Runner) logLine(job *Job, line string) {
	limit := 0
	if prov := r.Providers(); prov != nil {
		if s := prov.Settings(); s.KeepLogLines > 0 {
			limit = s.KeepLogLines
		}
	}
	job.lock()
	job.appendLogLocked(line, limit)
	job.unlock()
	r.emitJob(job)
	r.emitLog(job, []string{line})
}

// emitLog pushes log lines for a job to the frontend.
func (r *Runner) emitLog(job *Job, lines []string) {
	r.mu.Lock()
	emit := r.emit
	r.mu.Unlock()
	if emit != nil {
		emit(EventJobLog, LogBatch{JobID: job.ID, Lines: lines})
	}
}

// CancelAll cancels running jobs and marks the pending ones as cancelled. It also
// disarms the queue, so files added afterwards wait for 开始 instead of quietly
// starting themselves.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	r.armed = false
	r.paused = false
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
	// 停止 has to kill the frozen ones explicitly, for the same reason Shutdown
	// does: a cancelled context does not wake a suspended process.
	r.killSuspended()
	r.emitState()
}

// Stats reports queue counters.
func (r *Runner) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statsLocked()
}

func (r *Runner) statsLocked() Stats {
	s := Stats{Total: len(r.jobs), Paused: r.paused, Started: r.armed, Workers: r.workers}
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
		case StatusWarning:
			s.Warning++
		case StatusFailed:
			s.Failed++
		case StatusCanceled:
			s.Canceled++
		case StatusSkipped:
			s.Skipped++
		case StatusFiltered:
			s.Filtered++
		}
	}
	if s.Total > 0 {
		s.Progress = float64(s.Finished()) / float64(s.Total)
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
		if r.armed && !r.paused {
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
	// Filtering is entirely the template's business (or the global template's, for
	// a template that follows it). There is deliberately no queue-level override:
	// "this batch, these files only" looked like a queue decision but ended up as a
	// second, hidden copy of the same panel that silently outranked the template.
	filter := eff.Filter
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
				Dirs:      filter.Dir,
				Pattern:   filter.RenamePattern,
				Overwrite: filter.Overwrite,
				Copy:      filter.Action == store.ActionCopy,
				Template:  tpl.Name,
				Index:     job.Index,
			})
			// 筛选规则也会把源文件搬走，落点照样要记：不然这一行的「定位源文件」
			// 永远只有「文件不在这里了」这一句。只记真的搬成功的那次 —— 搬失败
			// 的文件还在原处，那才是它该被定位到的地方。
			if mErr == nil {
				r.markSourceMoved(job, filter.Action, dest)
			}
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
			switch {
			case mErr != nil:
				job.Error = mErr.Error()
				job.Message = reason + "；" + verb + "失败: " + mErr.Error()
			case dest == "":
				// 目标算出来就是源文件自己（目录表达式留空、命名又没改），
				// 那就什么都没发生，不能报一句"已移动到它原来的位置"。
				job.Message = reason + "；" + verb + "目标与源文件相同，未改动"
			default:
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
		Info: info, Tpl: eff, SrcRoot: job.SourceRoot, Index: job.Index,
	})
	if err != nil {
		fail("%v", err)
		return
	}
	// 「已处理过的文件」: a finished run of this template already produced this exact
	// output, so processing it again would only redo the same work. The section
	// decides what happens to the SOURCE instead.
	if stop := r.handleProcessed(job, s, eff, out); stop {
		return
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

	// 「已处理过的文件」: a verified result is on disk, so this is the moment the
	// source counts as processed. Both halves happen here, and before the status
	// is written, so that a source that could not be filed away lands in Warnings
	// instead of being bolted onto a row that already said 已完成.
	//
	// The record is only kept when the section is on. It is what the section
	// reads, and a run that never looks at it does not need to fill it in.
	if eff.Existing != nil {
		r.noteProcessed(job.Input, job.TemplateID)
		r.fileProcessedSource(job, s, eff)
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

// processedKey identifies "this file, under this template" for the in-session
// record. The path is lowercased because that is exactly how the queue decides
// whether two paths are the same file (see AddInputs), so whatever the queue
// treats as one file also counts as one record here.
func processedKey(input, tplID string) string {
	return strings.ToLower(input) + "\x00" + tplID
}

// alreadyProcessed reports whether this file has already been encoded by this
// template, successfully, during this run of the app.
//
// Nothing on disk is consulted and nothing is written, so there is no way for a
// stray file next to a user's media to make this answer wrong. The cost is that
// the answer does not survive a restart -- which is the behaviour that was asked
// for: process a folder today, open the app tomorrow, and every file is encoded
// again rather than silently skipped.
func (r *Runner) alreadyProcessed(input, tplID string) bool {
	if input == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.processedThisRun[processedKey(input, tplID)]
}

// noteProcessed records a verified run. Only called after the output file has
// been stat'ed non-empty, so a failure, a cancel or a truncated result never
// gets to claim the file has been handled.
func (r *Runner) noteProcessed(input, tplID string) {
	if input == "" {
		return
	}
	r.mu.Lock()
	r.processedThisRun[processedKey(input, tplID)] = true
	r.mu.Unlock()
}

// dropProcessed forgets a record.
//
// Two things call it. Deleting an output forgets that file: the record says "a
// result exists", and it no longer does, so honouring it would leave the user
// with neither the old file nor a new one. Saving a template forgets everything
// that template did: the record is only ever true of the settings it was made
// with, and re-running a folder after changing the encoder has to encode, not
// report 已跳过.
func (r *Runner) dropProcessed(input, tplID string) {
	if input == "" {
		return
	}
	r.mu.Lock()
	delete(r.processedThisRun, processedKey(input, tplID))
	r.mu.Unlock()
}

// ForgetTemplate drops every record made with one template.
func (r *Runner) ForgetTemplate(tplID string) {
	r.mu.Lock()
	for k := range r.processedThisRun {
		if strings.HasSuffix(k, "\x00"+tplID) {
			delete(r.processedThisRun, k)
		}
	}
	r.mu.Unlock()
}

// ForgetAllProcessed drops every record. Used when the global template changes,
// because it feeds the defaults of every other template.
func (r *Runner) ForgetAllProcessed() {
	r.mu.Lock()
	r.processedThisRun = map[string]bool{}
	r.mu.Unlock()
}

// handleProcessed applies the 「已处理过的文件」policy.
//
// The trigger is this run's own bookkeeping: the file went through this template
// already and came out the other side with a verified output. Nothing about the
// file sitting at the output path is consulted, because "there is a file there"
// says nothing about who produced it -- it fired for anything the user happened
// to have at that path, which is how the section ended up skipping files that
// had never been near this app.
//
// The policy acts on the SOURCE file, not on the output: 留在原处 is the plain
// "leave it alone" case, and 移动 / 复制 put the source somewhere else so the
// source tree stops feeding the same file back in. Either way this job is done --
// the point is to not run ffmpeg again, so there is no path here that continues
// to encoding.
//
// It reports whether the job is finished (skipped or failed), in which case the
// caller must return without running ffmpeg.
func (r *Runner) handleProcessed(job *Job, s store.Settings, tpl store.Template, out string) bool {
	ex := tpl.Existing
	if ex == nil {
		return false
	}
	if !r.alreadyProcessed(job.Input, job.TemplateID) {
		return false
	}

	src := job.Input
	dest, verb, err := r.existingRelocate(job, tpl, ex)
	if err != nil {
		// The file is recorded as processed but cannot be filed away. Failing is
		// the honest outcome: silently skipping would look like the rule ran.
		return r.stopJob(job, s, StatusFailed, "按「已处理过的文件」处理失败: "+err.Error())
	}
	if dest != "" {
		r.log(job, s, "[processed] 源文件已"+verb+"到 "+dest)
		src = dest
		r.markSourceMoved(job, ex.Action, dest)
	}

	// 「留在原处」and the two relocations all end the same way: this file has
	// already been through the template, so it is not processed a second time.
	reason := "本次运行已用该模板处理过，已跳过"
	if src != job.Input {
		reason = "本次运行已用该模板处理过，源文件已移走，本次跳过"
	}
	job.lock()
	job.Output = out
	job.OutputName = filepath.Base(out)
	job.Status = StatusSkipped
	job.Message = reason
	job.EndedAt = time.Now()
	job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	job.unlock()
	r.log(job, s, "[processed] "+reason)
	r.emitJob(job)
	r.record(job, s, StatusSkipped, reason)
	return true
}

// markSourceMoved remembers where a moved source ended up.
//
// Two rules file a source away -- 「已处理过的文件」 and the filter's 移动到目标目录 --
// and the list's 「定位源文件」 has one answer to give, so the destination is written
// here and nowhere else, whichever rule moved it. Without the filter half, a filtered
// row's 定位源文件 only ever says "文件不在这里了" while the file sits at the path that
// same row records as its output.
//
// 复制 is skipped by both: the original is still there, so the row's own path is still
// the right answer and a second one would only be a way to open the wrong file.
func (r *Runner) markSourceMoved(job *Job, action, dest string) {
	if dest == "" || action != store.ActionMove {
		return
	}
	job.lock()
	job.SourceMovedTo = dest
	job.unlock()
}

// existingRelocate moves or copies the SOURCE of a file this template has
// processed, per the 「已处理过的文件」 section. It returns the destination and the
// verb for the log line, or an empty destination when the section says to leave
// the file where it is.
//
// Both halves of the policy come through here -- the skip path (the file was
// processed in an earlier session) and the finish path (it was processed just
// now) -- so that 「移动到目标目录」 cannot end up meaning two different things.
func (r *Runner) existingRelocate(job *Job, tpl store.Template, ex *store.ExistingSpec) (dest, verb string, err error) {
	action := ex.Action
	if action != store.ActionMove && action != store.ActionCopy {
		return "", "", nil
	}
	verb = "移动"
	if action == store.ActionCopy {
		verb = "复制"
	}
	dest, err = Relocate(MoveRequest{
		Src:     job.Input,
		SrcRoot: job.SourceRoot,
		Dirs:    ex.Dir,
		// The source file was never re-encoded, so {ext} is its own extension --
		// the same rule the problem-file policies use.
		Pattern:   ex.Pattern,
		Overwrite: ex.Overwrite,
		Copy:      action == store.ActionCopy,
		Template:  tpl.Name,
		Index:     job.Index,
	})
	if err != nil {
		return "", verb, err
	}
	return dest, verb, nil
}

// fileProcessedSource applies the 「已处理过的文件」 relocation to the source of a
// run that has just produced a verified output.
//
// This is the half that never used to happen. The action only ran on the skip
// path, and a skip needs a trigger a first pass cannot have, so setting
// 「移动到目标目录」 changed nothing at all: the encode ran, the source stayed
// where it was, and the section looked broken.
//
// A failure is reported as a warning rather than swallowing it: the encode did
// work, so the job is still a success, but the user asked for the source to be
// filed away and it is not.
func (r *Runner) fileProcessedSource(job *Job, s store.Settings, tpl store.Template) {
	ex := tpl.Existing
	if ex == nil {
		return
	}
	if _, err := os.Stat(job.Input); err != nil {
		// The source went away during the encode -- nothing to file away, and
		// nothing worth a warning either.
		return
	}
	dest, verb, err := r.existingRelocate(job, tpl, ex)
	if err != nil {
		r.log(job, s, "[processed] 源文件处理失败: "+err.Error())
		job.lock()
		job.addWarningLocked("按「已处理过的文件」处理源文件失败: " + err.Error())
		job.unlock()
		return
	}
	if dest != "" {
		r.log(job, s, "[processed] 源文件已"+verb+"到 "+dest)
		r.markSourceMoved(job, ex.Action, dest)
	}
}

// stopJob ends a job that cannot run, with the same bookkeeping everywhere: one
// locked state write, one log line, one broadcast, one history row. Rolling that
// out by hand four times per policy is how the fields start drifting apart.
func (r *Runner) stopJob(job *Job, s store.Settings, status Status, reason string) bool {
	job.lock()
	job.Status = status
	job.Error = reason
	job.Message = reason
	job.EndedAt = time.Now()
	job.ElapsedMS = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	job.unlock()
	r.log(job, s, "[processed] "+reason)
	r.emitJob(job)
	r.record(job, s, status, reason)
	return true
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
		Src:     src,
		SrcRoot: job.SourceRoot,
		Dirs:    spec.Dir(statusKey),
		// Relocate falls back to "{name}.{ext}" for an empty pattern, which is
		// exactly the old behaviour: keep the file's own name.
		Pattern:  spec.Pattern(statusKey),
		Copy:     action == store.ActionCopy,
		Template: tpl.Name,
		Index:    job.Index,
	})
	if err != nil {
		r.log(job, s, "[policy] 文件处理失败: "+err.Error())
		job.lock()
		job.addWarningLocked("按策略处理源文件失败: " + err.Error())
		job.unlock()
		r.emitJob(job)
		return
	}
	if dest == "" {
		// 目标就是源文件自己，没有要报的搬迁。
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
	// Publish the pid so 暂停 can freeze the process.
	job.lock()
	job.pid = cmd.Process.Pid
	job.unlock()
	defer func() {
		job.lock()
		job.pid = 0
		job.Frozen = false
		// A handle that survived here means the process finished while frozen --
		// impossible in practice, but leaking one would pin the process object for
		// the rest of the session, so it is closed rather than assumed away.
		orphan := job.suspend
		job.suspend = nil
		job.unlock()
		orphan.Close()
	}()

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
	r.emitLog(job, []string{line})
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
		ID:            newJobID(),
		Input:         snap.Input,
		Output:        snap.Output,
		SourceMovedTo: snap.SourceMovedTo,
		TemplateID:    snap.TemplateID,
		TemplateName:  snap.TemplateName,
		Command:       snap.Command,
		Status:        status.Label(),
		Note:          note,
		Error:         snap.Error,
		Warnings:      snap.Warnings,
		StartedAt:     snap.StartedAt,
		EndedAt:       snap.EndedAt,
		ElapsedMS:     snap.ElapsedMS,
		Speed:         snap.Speed,
		Before:        summaryOf(snap.InfoBefore),
		After:         summaryOf(snap.InfoAfter),
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
