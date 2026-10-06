//go:build windows

package sysx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// The pause feature needs a child process whose progress is observable from the
// outside, that holds an open handle, and that stops when it stops running --
// which is exactly what ffmpeg is. The test binary re-executes itself for this
// rather than shelling out to a helper script: no dependency on anything being
// installed, and the loop is a real Go loop rather than a shell construct whose
// output buffering would decide whether the test passes.
const helperEnv = "FFMPEGGUI_SUSPEND_HELPER"

// TestHelperProcess is not a test. When the parent starts it with the env var set,
// it appends a line to the file named in the var, on a loop, forever.
func TestHelperProcess(t *testing.T) {
	path := os.Getenv(helperEnv)
	if path == "" {
		t.Skip("not a helper invocation")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	// Sync per line: the whole test is "did this file stop growing", so a buffered
	// writer would make a broken freeze look like a working one.
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

// startHelper launches the ticker and waits until it has produced its file, so a
// later measurement never races process startup.
func startHelper(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ticks.txt")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.timeout=10m")
	cmd.Env = append(os.Environ(), helperEnv+"="+out)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitForGrowth(t, out, 1)
	return cmd, out
}

// waitForGrowth blocks until the tick file has at least n lines.
func waitForGrowth(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if lineCount(t, path) >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper process never wrote %d lines to %s", n, path)
}

func lineCount(t *testing.T, path string) int {
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

// pidAlive is the independent liveness probe: it opens the pid without going
// through any SuspendProcess handle, so it cannot be fooled by our own bookkeeping.
func pidAlive(pid int) bool {
	h, _, _ := procOpenProcess.Call(uintptr(processSuspendResume|processQueryLimited), 0, uintptr(uint32(pid)))
	if h == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var code uint32
	if ret, _, _ := procGetExitCode.Call(uintptr(h), uintptr(unsafe.Pointer(&code))); ret == 0 {
		return false
	}
	return code == stillActive
}

// The core claim of the whole pause feature: a suspended process stops doing work
// and picks up exactly where it was, rather than restarting or dying.
func TestSuspendFreezesAndResumeContinues(t *testing.T) {
	cmd, ticks := startHelper(t)

	// Let it get going so there is a before/after to compare.
	time.Sleep(300 * time.Millisecond)
	before := lineCount(t, ticks)

	s, err := Suspend(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if !s.Alive() {
		t.Fatal("a just-suspended process must report as alive")
	}

	// Give it many ticks' worth of time. If the freeze did nothing, this is where
	// the line count moves.
	frozen := lineCount(t, ticks)
	time.Sleep(1500 * time.Millisecond)
	if got := lineCount(t, ticks); got > frozen+1 {
		t.Errorf("process kept writing while suspended: %d -> %d lines (was %d before the freeze)",
			frozen, got, before)
	}

	if err := s.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// And it must actually continue, not merely stop returning errors.
	waitForGrowth(t, ticks, frozen+3)
}

// Pausing twice must not need two resumes. The kernel keeps a suspend count, and a
// single Resume would leave the process frozen forever -- with the UI showing
// 「已继续」 and the bar stuck. That is why Pause skips a job that already holds a
// handle; this pins the primitive that makes that check necessary.
func TestDoubleSuspendNeedsDoubleResume(t *testing.T) {
	cmd, ticks := startHelper(t)
	waitForGrowth(t, ticks, 3)

	s1, err := Suspend(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("first Suspend: %v", err)
	}
	if _, err := Suspend(cmd.Process.Pid); err != nil {
		t.Fatalf("second Suspend: %v", err)
	}
	if err := s1.Resume(); err != nil {
		t.Fatalf("first Resume: %v", err)
	}

	time.Sleep(600 * time.Millisecond)
	mid := lineCount(t, ticks)
	time.Sleep(600 * time.Millisecond)
	if got := lineCount(t, ticks); got > mid+1 {
		t.Errorf("one Resume thawed a process suspended twice: %d -> %d lines", mid, got)
	}
}

// Resume must not decrement the suspend count of something it did not freeze --
// otherwise a second 继续 press thaws a process frozen by something else.
func TestResumeOnSpentHandleIsNoOp(t *testing.T) {
	cmd, _ := startHelper(t)

	s, err := Suspend(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	// Second call: the handle is spent, so this is the no-op path.
	if err := s.Resume(); err != nil {
		t.Errorf("resuming an already-thawed handle must be a no-op, got %v", err)
	}
}

// The shutdown path. A suspended process has no runnable thread, so it cannot act
// on a cancelled context or a closing pipe: without an explicit TerminateProcess it
// stays alive holding the output file, and quitting the app leaves the user with an
// orphan ffmpeg.exe.
func TestKillTerminatesSuspendedProcess(t *testing.T) {
	cmd, _ := startHelper(t)

	pid := cmd.Process.Pid
	if !pidAlive(pid) {
		t.Fatal("precondition: the helper should be running")
	}
	s, err := Suspend(pid)
	if err != nil {
		t.Fatal(err)
	}

	s.Kill()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("process %s survived Kill on a suspended handle", strconv.Itoa(pid))
}

// Kill must be safe on a spent handle: both Shutdown and the execFFmpeg defer can
// reach the same process.
func TestKillAfterResumeIsSafe(t *testing.T) {
	cmd, _ := startHelper(t)

	s, err := Suspend(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	s.Kill()
	s.Kill()
}

// A process that exits while frozen -- the user can do that from the task manager --
// must not make the next resume hang or panic. Either answer is acceptable; the
// point is that it returns.
func TestResumeAfterProcessExit(t *testing.T) {
	cmd, _ := startHelper(t)
	s, err := Suspend(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	time.Sleep(300 * time.Millisecond)

	if err := s.Resume(); err != nil && !strings.Contains(err.Error(), "恢复进程失败") {
		t.Errorf("unexpected error shape: %v", err)
	}
}

// Suspend must reject a pid that is not running rather than reporting success, or
// Pause would claim a job is frozen when its process had already exited.
func TestSuspendDeadPIDFails(t *testing.T) {
	if _, err := Suspend(0); err == nil {
		t.Error("pid 0 must be rejected")
	}

	// A pid that cannot be ours: a process that has certainly exited.
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperExitsImmediately$")
	cmd.Env = append(os.Environ(), helperEnv+"=") // empty path -> the helper skips
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper should exit cleanly, got %v", err)
	}
	if _, err := Suspend(cmd.Process.Pid); err == nil {
		t.Error("suspending an exited process must report an error")
	}
}

// TestHelperExitsImmediately exists only to hand the test above a pid that is
// guaranteed to have exited.
func TestHelperExitsImmediately(t *testing.T) {
	t.Skip("used only to obtain an exited pid")
}
