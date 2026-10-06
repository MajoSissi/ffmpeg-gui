//go:build windows

package sysx

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	ntdll                = syscall.NewLazyDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
	// kernel32 is declared once per package in power_windows.go.
	procOpenProcess      = kernel32.NewProc("OpenProcess")
	procGetExitCode      = kernel32.NewProc("GetExitCodeProcess")
	procTerminateProcess = kernel32.NewProc("TerminateProcess")
)

const (
	// PROCESS_TERMINATE is what lets a frozen process be killed at shutdown: a
	// process with no threads running is still killable, but only with this right.
	processTerminate = 0x0001
	// PROCESS_SUSPEND_RESUME is the only other access right needed here.
	processSuspendResume = 0x0800
	// PROCESS_QUERY_LIMITED_INFORMATION is what makes GetExitCodeProcess work, and
	// therefore what Alive() depends on. Without it the call fails with
	// ERROR_ACCESS_DENIED and Alive reports every frozen process as dead -- which is
	// the worst possible direction for the one function whose job is to tell the two
	// apart.
	processQueryLimited = 0x1000
	// NT_SUCCESS: the NTSTATUS half of the Win32 world spells "no error" as 0.
	ntSuccess = 0
	// ERROR_ACCESS_DENIED comes back when the process is gone or already exiting.
	errorAccessDenied = 5
	// STILL_ACTIVE is the exit code GetExitCodeProcess reports for a process that
	// has not exited yet.
	stillActive = 259
)

// SuspendedProcess freezes and thaws one child process.
//
// Why this exists: ffmpeg has no pause verb. The alternatives are worse -- killing
// it throws away the encoded frames, and a "restart from the last progress tick"
// would need a second encode pass plus a container-level join, producing a file
// that is not the same one. Freezing the process is the only thing that gives a
// real pause: verified against a 1080p30 encode, out_time_ms resumed 14s -> 22s
// and the final file was byte-identical in size to a control run of the same
// encoding time.
//
// The handle must be closed when done: leaving one open pins the process object.
type SuspendedProcess struct {
	handle syscall.Handle
	// frozen is the mirror of the process's actual state. Resume on a process that
	// is not frozen decrements its suspend count, which silently un-freezes a
	// process someone else froze -- so this is not just bookkeeping.
	frozen bool
}

// Suspend freezes the process with the given pid. It reports whether the process
// is now frozen; a false with a nil error means it had already exited.
func Suspend(pid int) (*SuspendedProcess, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("进程号无效：%d", pid)
	}
	h, _, err := procOpenProcess.Call(
		uintptr(processSuspendResume|processTerminate|processQueryLimited), 0, uintptr(uint32(pid)))
	if h == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errorAccessDenied {
			return nil, fmt.Errorf("进程 %d 已退出或无权限访问", pid)
		}
		return nil, fmt.Errorf("无法打开进程 %d：%w", pid, err)
	}
	handle := syscall.Handle(h)
	if status, _, _ := procNtSuspendProcess.Call(uintptr(handle)); int32(status) != ntSuccess {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("挂起进程 %d 失败（状态 0x%08X）", pid, status)
	}
	return &SuspendedProcess{handle: handle, frozen: true}, nil
}

// Resume thaws the process. Calling it on something that is not frozen is a no-op
// rather than an error: the process may have exited while it was paused, and the
// caller has nothing useful to do about it either way.
func (s *SuspendedProcess) Resume() error {
	if s == nil || s.handle == 0 {
		return nil
	}
	defer func() {
		s.frozen = false
		syscall.CloseHandle(s.handle)
		s.handle = 0
	}()
	if !s.frozen {
		return nil
	}
	if status, _, _ := procNtResumeProcess.Call(uintptr(s.handle)); int32(status) != ntSuccess {
		return fmt.Errorf("恢复进程失败（状态 0x%08X）", status)
	}
	return nil
}

// Alive reports whether the process is still running. It matters on the resume
// path: the user may well have quit the app while a job was frozen, which means
// this process is being torn down while it holds a suspended handle.
func (s *SuspendedProcess) Alive() bool {
	if s == nil || s.handle == 0 {
		return false
	}
	var code uint32
	if ret, _, _ := procGetExitCode.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&code))); ret == 0 {
		return false
	}
	return code == stillActive
}

// ProcessAlive reports whether a pid names a running process.
//
// Exported because the pause tests need to verify from the outside that a kill
// really happened. It deliberately opens the pid independently rather than reusing
// any SuspendedProcess handle: a check that shared the runner's own state could not
// catch the runner being wrong about that state.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(
		uintptr(processQueryLimited), 0, uintptr(uint32(pid)))
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

// Kill terminates the frozen process and releases the handle.
//
// This is the shutdown path. A suspended ffmpeg will never wake up on its own, so
// closing the app with one frozen would leave an orphan burning CPU while holding
// the output file open -- the user would see the program is gone and still find
// ffmpeg.exe in the task manager.
func (s *SuspendedProcess) Kill() {
	if s == nil || s.handle == 0 {
		return
	}
	procTerminateProcess.Call(uintptr(s.handle), 1)
	s.Close()
}

// Close releases the handle without thawing. Only for paths where the process is
// being killed anyway (shutdown) -- a frozen process left behind is a process that
// never exits.
func (s *SuspendedProcess) Close() {
	if s == nil || s.handle == 0 {
		return
	}
	syscall.CloseHandle(s.handle)
	s.handle = 0
	s.frozen = false
}
