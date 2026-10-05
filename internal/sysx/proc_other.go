//go:build !windows

package sysx

import (
	"os/exec"
	"runtime"
)

// reveal / openPath hand the path to the platform's file manager.
//
// No window-hiding attributes are applied: `open` and `xdg-open` are launchers,
// and suppressing their window suppresses the thing the user asked for.
func reveal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return launch("open", "-R", path)
	default:
		return launch("xdg-open", path)
	}
}

func openPath(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return launch("open", path)
	default:
		return launch("xdg-open", path)
	}
}

// launch starts a program and returns without waiting for it to exit.
func launch(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
