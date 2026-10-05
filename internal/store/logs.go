package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Run logs live in <data>/logs as one file per finished task. Two independent
// limits keep the folder from growing without bound — an age limit
// (LogKeepDays) and a size limit (LogMaxSizeMB) — and both are re-applied after
// every write, so the folder never drifts far past the configured budget.

// logTimeLayout is the leading, sortable timestamp of a log file name.
const logTimeLayout = "20060102-150405"

// LogFileName builds a sortable, human-readable name for one run.
func LogFileName(startedAt time.Time, label string) string {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if label = sanitizeLogLabel(label); label == "" {
		label = "task"
	}
	return startedAt.Format(logTimeLayout) + "_" + label + ".log"
}

func sanitizeLogLabel(s string) string {
	s = filepath.Base(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, filepath.Ext(s))
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
}

// LogUsage reports how many files the log folder holds and their total size.
func LogUsage(dir string) (count int, bytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() || !isLogName(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		count++
		bytes += fi.Size()
	}
	return count, bytes
}

func isLogName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".log")
}

// PruneLogs enforces the age and size limits and returns how many files were
// removed. Individual failures are skipped rather than aborting the sweep: one
// locked file must not stop the rest of the folder from being trimmed.
func PruneLogs(dir string, maxBytes int64, keepDays int) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	type item struct {
		path string
		mod  time.Time
		size int64
	}
	files := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isLogName(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, item{
			path: filepath.Join(dir, e.Name()),
			mod:  fi.ModTime(),
			size: fi.Size(),
		})
	}

	removed := 0

	// 1) age — anything older than the retention window goes first.
	if keepDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -keepDays)
		kept := files[:0]
		for _, f := range files {
			if f.mod.Before(cutoff) {
				if os.Remove(f.path) == nil {
					removed++
				}
				continue
			}
			kept = append(kept, f)
		}
		files = kept
	}

	// 2) total size — drop the oldest entries until the budget is met.
	if maxBytes > 0 {
		sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
		var total int64
		for _, f := range files {
			total += f.size
		}
		for i, f := range files {
			if total <= maxBytes {
				break
			}
			// Always keep the newest log. A single run can legitimately exceed
			// the whole budget, and deleting the file we just wrote would make
			// the feature silently useless.
			if i == len(files)-1 {
				break
			}
			if os.Remove(f.path) == nil {
				total -= f.size
				removed++
			}
		}
	}
	return removed
}

// WriteRunLog saves one task's log lines and then trims the folder.
//
// maxBytes caps both the single file and the folder total: a runaway task
// stops being recorded once it alone would consume the whole budget, instead of
// filling the disk before the next prune runs.
func WriteRunLog(dir string, when time.Time, label string, lines []string, maxBytes int64, keepDays int) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("日志目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建日志目录: %w", err)
	}
	path := uniqueLogPath(dir, LogFileName(when, label))

	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(ln)
		b.WriteByte('\n')
		if maxBytes > 0 && int64(b.Len()) >= maxBytes {
			b.WriteString("… 日志已达单文件大小上限，后续内容未写入\n")
			break
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("写入日志失败: %w", err)
	}

	PruneLogs(dir, maxBytes, keepDays)
	return path, nil
}

// uniqueLogPath appends -2, -3 … when two tasks finish within the same second.
func uniqueLogPath(dir, name string) string {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return path
	}
	base := strings.TrimSuffix(path, ".log")
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d.log", base, i)
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
}
