package engine

import (
	"os"
	"path/filepath"
	"testing"

	"ffmpeggui/internal/store"
)

// The sidecar is what tells "an interrupted run left this file" apart from "this
// file is a finished result". Without it a paused-then-quit encode leaves tens of
// megabytes of truncated output that handleProcessed would happily call done, and
// the user's re-run is skipped without a word.
func TestPartialMarkerLifecycle(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.mp4")

	writeFile(t, out, "half an encode")
	if isPartial(out) {
		t.Fatal("a plain finished-looking file must not read as partial")
	}

	markPartial(out)
	if !isPartial(out) {
		t.Fatal("markPartial did not leave a marker behind")
	}
	if _, err := os.Stat(partialMarker(out)); err != nil {
		t.Fatalf("no sidecar file on disk: %v", err)
	}

	clearPartial(out)
	if isPartial(out) {
		t.Fatal("clearPartial left the marker behind")
	}
	if _, err := os.Stat(partialMarker(out)); err == nil {
		t.Fatal("the sidecar file survived clearPartial")
	}
}

// The marker is matched on the resolved path too. ResolveOutput composes the
// destination from the source root, so a queue whose root is relative produces a
// different spelling of the same file -- and a marker that failed to match there
// would re-encode (and overwrite) a perfectly good result.
func TestPartialMarkerMatchesResolvedPath(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.mp4")
	writeFile(t, out, "result")
	markPartial(out)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	rel := filepath.Join(".", "clip.mp4")
	if !isPartial(rel) {
		t.Fatal("a relative spelling of the same output must still find its marker")
	}
}

// The whole reason the marker exists: a paused job that gets killed leaves a
// non-empty file behind, and the next run must encode it again rather than treat
// it as a previous result.
func TestHandleProcessedRetriesInterruptedOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")

	// Big enough that no zero-byte check would catch it -- the truncation is what
	// makes this case dangerous, not the emptiness.
	writeFile(t, out, "0123456789partial encode that never finished")
	markPartial(out)

	r, job := newPolicyRunner(t, src, dir)
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); stop {
		t.Fatal("an interrupted output must be re-encoded, not skipped as 已处理过")
	}
	// The marker was consumed rather than left to make the next run skip too.
	if isPartial(out) {
		t.Error("the marker should be cleared once the retry has been decided")
	}
}

// A genuinely finished result keeps the skip behaviour even when a stale marker
// from some earlier aborted attempt is lying around next to it. Getting this wrong
// costs the user a full re-encode, so it is worth pinning.
func TestHandleProcessedStaleMarkerDoesNotBlockForever(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "a complete result from a run that has since finished")
	markPartial(out)

	// Simulate what happens when the app is killed in the sliver between ffmpeg
	// exiting and the runner clearing the sidecar: the output is already good, and
	// the next run clears the marker and re-encodes. Recoverable -- re-encoding is
	// just wasted work, whereas the opposite mistake loses the result.
	if existsNonEmpty(out) && isPartial(out) {
		clearPartial(out)
	}
	if isPartial(out) {
		t.Fatal("marker not cleared")
	}

	r, job := newPolicyRunner(t, src, dir)
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
		t.Fatal("once the marker is gone the finished output must skip as before")
	}
}

// existsNonEmpty is the one definition of "there is a real result here" -- both
// handleProcessed and the pruner depend on it meaning the same thing.
func TestExistsNonEmptyRejectsDirectoriesAndEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.mp4")
	writeFile(t, empty, "")
	if existsNonEmpty(empty) {
		t.Error("a zero-byte file is an interrupted run, not a result")
	}
	if existsNonEmpty(dir) {
		t.Error("a directory is not a result")
	}
	if existsNonEmpty(filepath.Join(dir, "missing.mp4")) {
		t.Error("a missing file is not a result")
	}

	good := filepath.Join(dir, "good.mp4")
	writeFile(t, good, "x")
	if !existsNonEmpty(good) {
		t.Error("a one-byte file is still a result")
	}
}
