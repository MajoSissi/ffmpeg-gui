//go:build windows

package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ffmpeggui/internal/sysx"
)

// The end-to-end shape of the pause feature: a job is running, the user presses
// 暂停, the underlying process genuinely stops, and 继续 makes it genuinely start
// again with its state intact.
//
// The child here is the test binary re-executing itself as a ticker rather than a
// real ffmpeg. That keeps the test hermetic -- no encode, no fixture video, no
// ffmpeg on the machine -- while still exercising the real syscall path against a
// real OS process with a real pid, which is the part that can actually be wrong.
const runnerHelperEnv = "FFMPEGGUI_RUNNER_HELPER"

func TestRunnerHelperProcess(t *testing.T) {
	path := os.Getenv(runnerHelperEnv)
	if path == "" {
		t.Skip("not a helper invocation")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	for i := 0; ; i++ {
		if _, err := fmt.Fprintf(f, "%d\n", i); err != nil {
			return
		}
		if err := f.Sync(); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startTicker returns a running child plus the file it appends to.
func startTicker(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ticks.txt")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerHelperProcess$", "-test.timeout=10m")
	cmd.Env = append(os.Environ(), runnerHelperEnv+"="+out)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitForLines(t, out, 1)
	return cmd, out
}

func waitForLines(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if helperLineCount(t, path) >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper never wrote %d lines", n)
}

func helperLineCount(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// pidAliveInEngine is the independent check that a kill really happened. It goes
// through sysx.ProcessAlive, which opens the pid itself rather than reusing a
// handle the runner kept, so it cannot be fooled by our own bookkeeping.
func pidAliveInEngine(pid int) bool { return sysx.ProcessAlive(pid) }

// runnerWithChild wires a runner with one job whose "ffmpeg process" is a real
// running child, and sets the pid the way execFFmpeg does.
func runnerWithChild(t *testing.T) (*Runner, *Job, *exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	cmd, ticks := startTicker(t)

	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	t.Cleanup(r.Shutdown)

	job := NewJob(filepath.Join(dir, "in.mp4"), dir, "t1", "x")
	job.Status = StatusRunning
	job.pid = cmd.Process.Pid
	r.jobs = []*Job{job}
	return r, job, cmd, ticks
}

// Pause must actually stop the work, and 继续 must actually restart it -- that is
// the whole feature. Anything less (queue held back, process left running) is the
// behaviour the user complained about.
func TestRunnerPauseFreezesAndResumeContinues(t *testing.T) {
	r, job, _, ticks := runnerWithChild(t)
	waitForLines(t, ticks, 3)

	r.Pause()
	if s := r.Stats(); !s.Paused {
		t.Fatal("暂停 did not set the queue flag")
	}
	if !job.Snapshot().Frozen {
		t.Error("暂停 did not mark the running job frozen, so the UI cannot show it")
	}

	frozen := helperLineCount(t, ticks)
	time.Sleep(1500 * time.Millisecond)
	if got := helperLineCount(t, ticks); got > frozen+1 {
		t.Errorf("the process kept working while paused: %d -> %d lines", frozen, got)
	}

	r.Resume()
	if s := r.Stats(); s.Paused {
		t.Fatal("继续 left the queue paused")
	}
	if job.Snapshot().Frozen {
		t.Error("继续 left the job marked frozen")
	}
	// And the work has to resume -- not merely stop erroring out.
	waitForLines(t, ticks, frozen+3)
}

// Pressing 暂停 twice must not need two 继续 presses. The kernel counts suspends,
// so a second freeze needs a second thaw, and a user who mashes the button would be
// told 「已继续」 while the bar stays stuck.
func TestRunnerPauseTwiceResumesOnce(t *testing.T) {
	r, job, _, ticks := runnerWithChild(t)
	waitForLines(t, ticks, 3)

	r.Pause()
	h := job.suspend
	if h == nil {
		t.Skip("this platform cannot freeze a process")
	}
	r.Pause()
	if job.suspend != h {
		t.Fatal("the second 暂停 replaced the handle instead of reusing it")
	}

	// One 继续 must be enough. If it were not, this test binary's own helper would
	// stay frozen and waitForLines would fail on the timeout rather than hang.
	r.Resume()
	waitForLines(t, ticks, helperLineCount(t, ticks)+3)
}

// 停止 must terminate a frozen process. Cancelling its context does nothing -- a
// suspended process has no runnable thread to observe the cancellation -- so without
// an explicit kill the user is left with an ffmpeg.exe still holding the output.
func TestRunnerCancelAllKillsFrozenProcess(t *testing.T) {
	r, job, cmd, _ := runnerWithChild(t)
	pid := cmd.Process.Pid

	r.Pause()
	if job.suspend == nil {
		t.Skip("this platform cannot freeze a process")
	}
	r.CancelAll()

	if job.Snapshot().Frozen {
		t.Error("停止 left the job frozen")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAliveInEngine(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("process %d survived 停止 while frozen", pid)
}

// Quitting the app with a frozen job must not leave an orphan behind. This is the
// single most user-visible consequence of the feature: without it, closing the
// window leaves a burning CPU core and a locked output file.
func TestRunnerShutdownKillsFrozenProcess(t *testing.T) {
	r, job, cmd, _ := runnerWithChild(t)
	pid := cmd.Process.Pid

	r.Pause()
	if job.suspend == nil {
		t.Skip("this platform cannot freeze a process")
	}
	r.Shutdown()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAliveInEngine(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("process %d survived shutdown while frozen", pid)
}

// When the thaw cannot actually happen -- the process was killed from the task
// manager while frozen -- the row must keep saying 「已暂停」 rather than going back
// to 「处理中」. Otherwise the user sees a running job with a bar that will never
// move again, and 继续 looks like it did nothing.
//
// This is the one assertion that cannot be made with a stand-in handle, because a
// stand-in Resume always succeeds; it needs the real syscall to fail.
func TestRunnerFailedThawKeepsTheFrozenBadge(t *testing.T) {
	r, job, cmd, _ := runnerWithChild(t)

	r.Pause()
	if job.suspend == nil {
		t.Skip("this platform cannot freeze a process")
	}

	// Kill it behind the runner's back, so NtResumeProcess has nothing to thaw.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	time.Sleep(300 * time.Millisecond)

	r.Resume()

	snap := job.Snapshot()
	if !snap.Frozen {
		t.Error("继续 cleared the badge even though the process could not be resumed")
	}
	if snap.Message != "已暂停（恢复失败）" {
		t.Errorf("message = %q, want the failure to be stated", snap.Message)
	}
}
