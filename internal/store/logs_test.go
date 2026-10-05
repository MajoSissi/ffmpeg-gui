package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name string, size int, mod time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return p
}

func TestPruneLogsByAge(t *testing.T) {
	dir := t.TempDir()
	old := write(t, dir, "old.log", 16, time.Now().AddDate(0, 0, -10))
	recent := write(t, dir, "recent.log", 16, time.Now().Add(-time.Hour))
	// A non-log file must never be touched, whatever its age.
	other := write(t, dir, "settings.json", 16, time.Now().AddDate(0, 0, -30))

	if n := PruneLogs(dir, 0, 7); n != 1 {
		t.Errorf("removed %d files, want 1", n)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("log older than the retention window survived")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("recent log was deleted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a non-log file was deleted")
	}
}

func TestPruneLogsBySizeDropsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldest := write(t, dir, "1.log", 1000, now.Add(-3*time.Hour))
	middle := write(t, dir, "2.log", 1000, now.Add(-2*time.Hour))
	newest := write(t, dir, "3.log", 1000, now.Add(-1*time.Hour))

	// Budget fits two of the three files.
	if n := PruneLogs(dir, 2500, 3650); n != 1 {
		t.Errorf("removed %d files, want 1", n)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Error("oldest file should have been removed first")
	}
	if _, err := os.Stat(middle); err != nil {
		t.Error("middle file should have survived")
	}
	if _, err := os.Stat(newest); err != nil {
		t.Error("newest file should have survived")
	}
}

func TestWriteRunLogStopsAtBudget(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("a", 100)
	lines := []string{line, line, line, line, line}

	path, err := WriteRunLog(dir, time.Now(), "clip.mp4", lines, 250, 7)
	if err != nil {
		t.Fatalf("WriteRunLog: %v", err)
	}
	if filepath.Ext(path) != ".log" {
		t.Errorf("unexpected extension: %s", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 250 byte budget plus the one-line truncation marker.
	if fi.Size() > 400 {
		t.Errorf("log grew to %d bytes, budget was 250", fi.Size())
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "上限") {
		t.Error("truncated log should say why it stopped")
	}
}

func TestWriteRunLogKeepsNamesUnique(t *testing.T) {
	dir := t.TempDir()
	when := time.Now()
	first, err := WriteRunLog(dir, when, "clip.mp4", []string{"a"}, 1<<20, 7)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteRunLog(dir, when, "clip.mp4", []string{"b"}, 1<<20, 7)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("two runs in the same second reused %s", first)
	}
}

func TestLogFileNameIsSortable(t *testing.T) {
	dir := t.TempDir()
	s := Settings{LogDir: dir}
	s.Normalize()

	early := LogFileName(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "a.mp4")
	late := LogFileName(time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC), "a.mp4")
	if !(early < late) {
		t.Errorf("names do not sort chronologically: %q vs %q", early, late)
	}
	if strings.ContainsAny(early, `<>:"/\|?*`) {
		t.Errorf("name has characters Windows rejects: %q", early)
	}
}
