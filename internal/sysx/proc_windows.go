//go:build windows

package sysx

import "syscall"

const createNoWindow = 0x08000000

// NoWindow returns process attributes that keep a console window from flashing
// when we shell out to ffmpeg / ffprobe and capture their output.
//
// Only ever use this for console programs. STARTF_USESHOWWINDOW combined with
// SW_HIDE is honoured by GUI programs too: they create their *first* window
// hidden, so a launcher would appear to do nothing at all. GUI launching no
// longer goes through CreateProcess in any case -- see shell_windows.go.
func NoWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
