package engine

import (
	"os"
	"path/filepath"
	"testing"

	"ffmpeggui/internal/store"
	"ffmpeggui/internal/sysx"
)

// These exercise the runner's side of the pause contract -- which jobs get frozen,
// which get the badge, and that nothing is left running afterwards -- without
// freezing anything. The real process work is in pause_windows_test.go and
// internal/sysx; what matters here is the bookkeeping, and it has to be testable
// on a machine where suspending a process would suspend the test run.
//
// inertHandle is a suspend handle that refers to nothing: Kill and Resume on it are
// both no-ops, which makes it the right stand-in for "a job that had a handle".
func inertHandle() *sysx.SuspendedProcess { return &sysx.SuspendedProcess{} }

// Pause must leave an already-frozen job's handle alone. The kernel counts
// suspends, so a second NtSuspendProcess needs a second NtResumeProcess to undo --
// a user mashing the pause button would otherwise be told 「已继续」 while the
// process stays frozen forever.
func TestPauseLeavesAlreadyFrozenJobsAlone(t *testing.T) {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)

	frozen := NewJob(filepath.Join(t.TempDir(), "a.mp4"), t.TempDir(), "t1", "x")
	frozen.pid = os.Getpid()
	frozen.Status = StatusRunning
	h := inertHandle()
	frozen.suspend = h
	frozen.Frozen = true

	// No pid: nothing to freeze, and an OpenProcess on pid 0 must never be tried.
	idle := NewJob(filepath.Join(t.TempDir(), "b.mp4"), t.TempDir(), "t1", "x")
	idle.Status = StatusPending

	r.jobs = []*Job{frozen, idle}
	r.Pause()

	if frozen.suspend != h {
		t.Error("Pause replaced an existing handle; it must reuse the one it has")
	}
	if !frozen.Frozen {
		t.Error("Pause cleared the frozen flag on a job it did not touch")
	}
	for _, j := range r.Jobs() {
		if j.ID == idle.ID && j.Frozen {
			t.Error("a job with no process was frozen")
		}
	}
}

// The badge is what makes a paused queue legible: without it the row says 「处理中」
// next to a progress bar that has stopped moving, which reads as a hang.
func TestFreezeFlagReachesTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	r, job := newPolicyRunner(t, filepath.Join(dir, "a.mp4"), dir)
	r.jobs = []*Job{job}

	if job.Snapshot().Frozen {
		t.Fatal("a fresh job must not report frozen")
	}
	job.lock()
	job.Frozen = true
	job.unlock()
	if !job.Snapshot().Frozen {
		t.Error("Snapshot dropped the frozen flag, so the UI could never show it")
	}
}

// Suspend handles must never travel to the frontend. A syscall.Handle marshalled
// into JSON is at best noise and at worst a stale handle something could act on.
func TestSnapshotDropsProcessHandles(t *testing.T) {
	dir := t.TempDir()
	r, job := newPolicyRunner(t, filepath.Join(dir, "a.mp4"), dir)
	r.jobs = []*Job{job}

	job.lock()
	job.pid = os.Getpid()
	job.suspend = inertHandle()
	job.unlock()

	snap := job.Snapshot()
	if snap.suspend != nil {
		t.Error("Snapshot leaked the suspend handle to the frontend")
	}
}

// 停止 and shutdown must terminate frozen processes, not merely close the pipes.
// A suspended process has no runnable thread: it cannot observe the cancellation,
// so without an explicit kill it stays alive holding the output file. This is the
// case that produced a user-visible orphan ffmpeg after quitting the app.
func TestKillSuspendedClearsEveryHandle(t *testing.T) {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)

	a := NewJob("a.mp4", ".", "t1", "x")
	b := NewJob("b.mp4", ".", "t1", "x")
	// A handle always comes with a pid: execFFmpeg publishes the pid before any
	// freeze can happen and clears both under one lock afterwards, so "has a
	// suspend handle" and "has a live pid" are the same state.
	a.suspend, b.suspend = inertHandle(), inertHandle()
	a.pid, b.pid = os.Getpid(), os.Getpid()
	a.Frozen, b.Frozen = true, true
	// A job with a pid but no handle is still running and must not be touched.
	c := NewJob("c.mp4", ".", "t1", "x")
	c.pid = os.Getpid()

	r.jobs = []*Job{a, b, c}

	if n := r.killSuspended(); n != 2 {
		t.Errorf("killSuspended reported %d, want 2", n)
	}
	for _, j := range r.Jobs() {
		if j.Frozen {
			t.Errorf("job %s still frozen after the queue was stopped", j.ID)
		}
		if j.ID == c.ID && j.pid == 0 {
			t.Error("killSuspended cleared the pid of a job it had no business touching")
		}
	}
}

// CancelAll is the 停止 button: it has to do the same cleanup as shutdown, or the
// user presses 停止 and finds ffmpeg still running.
func TestCancelAllKillsFrozenJobs(t *testing.T) {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	job := NewJob("a.mp4", ".", "t1", "x")
	job.suspend = inertHandle()
	job.pid = os.Getpid()
	job.Frozen = true
	r.jobs = []*Job{job}

	r.CancelAll()
	if job.Snapshot().Frozen {
		t.Error("停止 left a job frozen")
	}
}

// thawAll must be safe when nothing is frozen, and Resume must still clear the
// queue-level pause -- otherwise 继续 turns into a no-op that also leaves the
// queued jobs unable to start.
func TestResumeOnNothingFrozen(t *testing.T) {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	job := NewJob("a.mp4", ".", "t1", "x")
	job.Status = StatusPending
	r.jobs = []*Job{job}

	r.Pause()
	if n := r.thawAll(); n != 0 {
		t.Errorf("thawAll reported %d thawed jobs, want 0", n)
	}
	r.Resume()
	if s := r.Stats(); s.Paused {
		t.Error("继续 did not clear the paused flag")
	}
}

// A thaw that cannot happen must leave the badge on. Showing 「已暂停（恢复失败）」
// is the difference between the user pressing 继续 again and believing the job is
// running while its bar never moves. The failure needs a real syscall, so the
// driving of it lives in pause_windows_test.go; what is pinned here is that
// thawJob routes a failed thaw through markFrozen rather than clearing the badge
// unconditionally -- which is only checkable if the badge is consulted after the
// attempt, not before it.

// Shutdown with a frozen job in the queue must not deadlock or panic: it cancels
// the context (which cannot reach a suspended process) and then kills explicitly.
func TestShutdownWithFrozenJob(t *testing.T) {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	job := NewJob("a.mp4", ".", "t1", "x")
	job.suspend = inertHandle()
	job.pid = os.Getpid()
	job.Frozen = true
	r.jobs = []*Job{job}

	r.Shutdown()
	if s := r.Stats(); s.Total != 1 {
		t.Errorf("Shutdown dropped the queue: total=%d", s.Total)
	}
}

// Windows recycles pids. If a finished ffmpeg's number has already been handed to
// the next queue item's ffmpeg by the time 暂停 runs, attaching the handle by pid
// would freeze the wrong row's badge -- and 继续 would then thaw a process nobody
// had suspended, leaving the real one frozen with the UI claiming all is well.
//
// attachSuspend therefore checks the job still owns that pid and thaws the stray
// process instead of adopting it.
func TestAttachSuspendRejectsRecycledPid(t *testing.T) {
	dir := t.TempDir()
	r, job := newPolicyRunner(t, filepath.Join(dir, "in.mp4"), dir)
	job.pid = 4242
	job.Status = StatusRunning

	// The handle refers to nothing, so thawing it is a no-op -- what is being
	// checked is that it is NOT adopted onto the job.
	stray := inertHandle()
	r.attachSuspend(job, 9999, stray) // 9999 != job.pid

	if job.Snapshot().Frozen {
		t.Error("a handle for a different pid was adopted onto this job")
	}
	if job.suspend != nil {
		t.Error("a mismatched pid must not leave a suspend handle on the job")
	}
}

// The matching case: same pid, handle adopted, badge on.
func TestAttachSuspendAdoptsMatchingPid(t *testing.T) {
	dir := t.TempDir()
	r, job := newPolicyRunner(t, filepath.Join(dir, "in.mp4"), dir)
	job.pid = os.Getpid()
	job.Status = StatusRunning

	h := inertHandle()
	r.attachSuspend(job, os.Getpid(), h)

	if !job.Snapshot().Frozen {
		t.Error("a matching pid must set the frozen badge")
	}
	if job.suspend != h {
		t.Error("the handle was not recorded on the job")
	}
}

// misread as "an encode was interrupted here". Every caller checks for a real
// non-empty output first and bails out before reaching isPartial, so the leftover
// file is inert. This is why there is no pruner sweeping for stale sidecars.
func TestOrphanedMarkerIsInert(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	markPartial(out) // ...and then the output never materialised

	r, job := newPolicyRunner(t, src, dir)
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); stop {
		t.Error("a marker with no output must not skip the file")
	}
	// The marker stays put -- it belongs to a run the app knows nothing about, and
	// deleting it here would be guessing.
	if !isPartial(out) {
		t.Error("the orphaned marker was removed; it is not this function's to clean up")
	}
}

// A partial output must not stop the run: handleProcessed has to leave the job in
// its pre-pipeline state so the encoder starts on a job that does not already
// claim to have finished.
func TestPartialOutputDoesNotBlockRetry(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "truncated")
	markPartial(out)

	r, job := newPolicyRunner(t, src, dir)
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); stop {
		t.Fatal("the retry was skipped")
	}
	if job.Status == StatusSkipped {
		t.Error("job was marked skipped even though the output is a leftover")
	}
}
