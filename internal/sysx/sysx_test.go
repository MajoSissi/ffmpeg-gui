package sysx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests deliberately only exercise the path-validation path.
//
// Reveal/Open on a path that EXISTS would open a real Explorer window, and a test
// suite should never do that to whoever runs it. So the filesystem checks are
// tested directly, plus the guarantee that a bad path is refused *before* any
// shell call happens -- which is exactly the behaviour that used to make the
// "reveal" button open Documents instead of the file's folder.

func TestExistingPathRejectsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t"} {
		if _, err := existingPath(in); err == nil {
			t.Errorf("existingPath(%q) should fail", in)
		}
	}
}

func TestExistingPathRejectsMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", "gone.mp4")
	_, err := existingPath(missing)
	if err == nil {
		t.Fatal("a missing path must be rejected")
	}
	if !strings.Contains(err.Error(), "已不存在") {
		t.Errorf("error should say the file is gone, got: %v", err)
	}
}

func TestExistingPathAcceptsExistingAndAbsolutises(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "中文 文件名.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := existingPath(file)
	if err != nil {
		t.Fatalf("existing file should be accepted: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("result should be absolute, got %q", got)
	}
	if got != filepath.Clean(file) {
		t.Errorf("got %q, want %q", got, filepath.Clean(file))
	}

	// Trimming must not break a valid path.
	if got, err := existingPath("  " + file + "  "); err != nil || got != filepath.Clean(file) {
		t.Errorf("surrounding whitespace should be trimmed, got %q err=%v", got, err)
	}
}

func TestExistingPathAcceptsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := existingPath(dir); err != nil {
		t.Errorf("a directory is a valid target for Open: %v", err)
	}
}

func TestRevealAndOpenRefuseMissingPaths(t *testing.T) {
	// If the shell were reached, this would pop a real Explorer window on the
	// developer's desktop (and land in Documents). Asserting the error proves the
	// guard runs first.
	missing := filepath.Join(t.TempDir(), "not-here.mp4")

	for name, fn := range map[string]func(string) error{"Reveal": Reveal, "Open": Open} {
		err := fn(missing)
		if err == nil {
			t.Errorf("%s should refuse a missing path", name)
			continue
		}
		if !strings.Contains(err.Error(), "已不存在") {
			t.Errorf("%s error should mention the file is gone, got: %v", name, err)
		}
	}
}
