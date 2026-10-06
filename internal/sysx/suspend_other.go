//go:build !windows

package sysx

import "fmt"

// SuspendedProcess is a no-op placeholder off Windows. The app ships Windows-only
// (Wails + the tray both assume it), but the other files in this package keep
// their non-Windows counterparts, and a build error here would be reported as
// "pause does not exist" rather than "you built for the wrong target".
type SuspendedProcess struct{}

// Suspend always fails: the platform has no equivalent we can rely on. SIGSTOP
// exists on Unix, but a suspended ffmpeg holding a pipe this process reads would
// block the reader on resume, which is a different set of bugs for a platform
// nobody builds this on.
func Suspend(pid int) (*SuspendedProcess, error) {
	return nil, fmt.Errorf("当前平台不支持暂停正在处理的任务")
}

func (s *SuspendedProcess) Resume() error { return nil }
func (s *SuspendedProcess) Alive() bool   { return false }
func (s *SuspendedProcess) Kill()         {}
func (s *SuspendedProcess) Close()        {}
