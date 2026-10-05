// Package sysx holds small OS integrations: shell integration (opening and
// revealing files), keep-awake and CPU helpers.
package sysx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Reveal opens the file manager with the given path selected.
func Reveal(path string) error {
	abs, err := existingPath(path)
	if err != nil {
		return err
	}
	return reveal(abs)
}

// Open opens a file or directory with the default handler.
func Open(path string) error {
	abs, err := existingPath(path)
	if err != nil {
		return err
	}
	return openPath(abs)
}

// existingPath trims, absolutises and verifies a path before it reaches the OS.
//
// The check is not just defensive tidiness. Handing the shell a path that no
// longer exists is silently harmful on Windows: Explorer ignores the request and
// opens its own default folder (Documents or Desktop) instead, which reads as the
// button doing something random. Refusing here turns that into a real message.
//
// Errors are written for display -- they surface in the UI as a toast.
func existingPath(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", errors.New("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("路径无效：%s", p)
	}
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("文件已不存在，可能已被移动或删除：%s", abs)
		}
		return "", fmt.Errorf("无法访问：%s", abs)
	}
	return abs, nil
}

// ThreadCount reports the usable CPU count for default concurrency hints.
func ThreadCount() int { return runtime.NumCPU() }
