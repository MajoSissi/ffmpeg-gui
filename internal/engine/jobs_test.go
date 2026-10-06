package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveJobsDropsBatch(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)

	// AddInputs stats the path, so the fixtures have to exist on disk.
	for _, p := range []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4"} {
		full := filepath.Join(dir, p)
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		n, _ := r.AddInputs([]InputItem{{Path: full}}, "t1", "x")
		if n != 1 {
			t.Fatalf("failed to queue %s", p)
		}
	}
	if got := len(r.Jobs()); got != 4 {
		t.Fatalf("queued %d jobs, want 4", got)
	}

	ids := r.Jobs()
	// Two real ids plus one that does not exist: an unknown id must be skipped, not
	// reported as an error -- the row can vanish under the caller while the queue runs.
	n := r.RemoveJobs([]string{ids[0].ID, ids[2].ID, "does-not-exist"})
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	left := r.Jobs()
	if len(left) != 2 {
		t.Fatalf("%d jobs left, want 2", len(left))
	}
	for _, j := range left {
		if j.ID == ids[0].ID || j.ID == ids[2].ID {
			t.Errorf("job %s should have been removed", j.ID)
		}
	}

	if n := r.RemoveJobs(nil); n != 0 {
		t.Errorf("RemoveJobs(nil) = %d, want 0", n)
	}
	if n := r.RemoveJobs([]string{"", "  "}); n != 0 {
		t.Errorf("RemoveJobs(blank) = %d, want 0", n)
	}
	if got := len(r.Jobs()); got != 2 {
		t.Errorf("blank removals changed the queue: %d left", got)
	}
}

// Switching the toolbar's template binds the whole queue to it, so every row has
// to move -- including the ones that already ran, which go back to 排队中 so
// 开始 re-runs them with the new parameters. The one exception is a job already
// on the CPU: re-pointing it mid-flight would only produce a half-old, half-new
// file, so it keeps what it started with.
func TestUpdateAllTemplatesMovesEveryRow(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, p := range []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4"} {
		full := filepath.Join(dir, p)
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, full)
	}
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	items := make([]InputItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, InputItem{Path: p})
	}
	if n, errs := r.AddInputs(items, "old", "旧模板"); n != 4 || len(errs) != 0 {
		t.Fatalf("queued %d (errors %v), want 4", n, errs)
	}

	// One done, one failed, one mid-flight, one still queued.
	r.mu.Lock()
	onCPU := r.jobs[2].ID
	r.jobs[1].Status = StatusDone
	r.jobs[1].Progress = 1
	r.jobs[2].Status = StatusRunning
	r.jobs[3].Status = StatusFailed
	r.mu.Unlock()

	got := r.UpdateAllTemplates("new", "新模板")
	if got.Applied != 3 {
		t.Errorf("applied to %d jobs, want 3 (the running one is left alone)", got.Applied)
	}
	if got.Requeued != 2 {
		t.Errorf("requeued %d jobs, want 2 (done + failed)", got.Requeued)
	}
	for _, j := range r.Jobs() {
		if j.ID == onCPU {
			if j.TemplateID != "old" {
				t.Errorf("the running job was re-pointed to %q", j.TemplateID)
			}
			continue
		}
		if j.TemplateID != "new" || j.TemplateName != "新模板" {
			t.Errorf("job %s bound to %q/%q, want new/新模板", j.ID, j.TemplateID, j.TemplateName)
		}
		if j.Status != StatusPending {
			t.Errorf("job %s is %s, want pending", j.ID, j.Status)
		}
		if j.Progress != 0 {
			t.Errorf("job %s kept progress %v", j.ID, j.Progress)
		}
	}
}

// A file named outright used to skip the extension gate a scanned directory
// applies, so dragging a .zip onto the window queued a job ffmpeg could never
// open: it counted towards the totals but had no row to show for it.
func TestAddInputsRejectsFilesFFmpegCannotOpen(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "clip.mp4")
	zip := filepath.Join(dir, "archive.zip")
	for _, p := range []string{real, zip} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	n, errs := r.AddInputs([]InputItem{{Path: real}, {Path: zip}}, "t1", "x")
	if n != 1 {
		t.Fatalf("added %d, want 1 (the zip is not media)", n)
	}
	if len(errs) != 1 {
		t.Fatalf("errors %v, want exactly one", errs)
	}
	if !strings.Contains(errs[0], "不是媒体文件") {
		t.Errorf("error %q does not say why the file was skipped", errs[0])
	}
	jobs := r.Jobs()
	if len(jobs) != 1 || jobs[0].Input != real {
		t.Errorf("queue is %+v, want just the mp4", jobs)
	}
}

// Removing a running job must cancel it, or the worker would keep an ffmpeg process
// alive writing to a file the user just deleted from the queue.
func TestRemoveJobsSkipsAbsentFilesGracefully(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.mp4")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	if n, _ := r.AddInputs([]InputItem{{Path: real}}, "t1", "x"); n != 1 {
		t.Fatal("failed to queue the fixture")
	}
	if n := r.RemoveJobs([]string{r.Jobs()[0].ID, "ghost"}); n != 1 {
		t.Errorf("removed %d, want 1", n)
	}
	if len(r.Jobs()) != 0 {
		t.Errorf("queue should be empty, has %d", len(r.Jobs()))
	}
}

// {index} has to expand to the same number in the queue preview and in the real
// run. The preview used to compute it from the slice position and the runner never
// passed one at all, so a pattern containing {index} produced a different file
// name in each place.
func TestAddInputsNumbersJobsFromOne(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, p := range []string{"a.mp4", "b.mp4", "c.mp4"} {
		full := filepath.Join(dir, p)
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, full)
	}

	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	items := make([]InputItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, InputItem{Path: p})
	}
	if n, errs := r.AddInputs(items, "t1", "x"); n != 3 || len(errs) != 0 {
		t.Fatalf("queued %d (errors %v), want 3", n, errs)
	}
	for i, j := range r.Jobs() {
		if j.Index != i+1 {
			t.Errorf("job %d has Index %d, want %d", i, j.Index, i+1)
		}
	}

	// A second batch keeps counting instead of restarting...
	more := filepath.Join(dir, "d.mp4")
	if err := os.WriteFile(more, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.AddInputs([]InputItem{{Path: more}}, "t1", "x")
	jobs := r.Jobs()
	if len(jobs) != 4 || jobs[3].Index != 4 {
		t.Fatalf("after the second batch: %d jobs, last Index %d (want 4)", len(jobs), jobs[len(jobs)-1].Index)
	}

	// ...and removing a job does not renumber the rest: an index that moved under
	// the user would rename files that were already written.
	r.RemoveJobs([]string{jobs[1].ID})
	for _, want := range []struct {
		name string
		idx  int
	}{{"a.mp4", 1}, {"c.mp4", 3}, {"d.mp4", 4}} {
		found := false
		for _, j := range r.Jobs() {
			if j.InputName == want.name {
				found = true
				if j.Index != want.idx {
					t.Errorf("%s has Index %d, want %d after the removal", want.name, j.Index, want.idx)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from the queue", want.name)
		}
	}
}

// A queue only runs once the user presses 开始. Dropping a file on the window used
// to be enough: the worker pool is built when a template loads, so any pending job
// was taken immediately, and there was no way back short of 停止.
func TestQueueWaitsForStart(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	// The pool exists; nothing has been started.
	r.EnsureWorkers(1)

	if n, _ := r.AddInputs([]InputItem{{Path: full}}, "t1", "x"); n != 1 {
		t.Fatal("the file was not queued")
	}
	time.Sleep(150 * time.Millisecond)
	if got := r.Jobs()[0].Status; got != StatusPending {
		t.Fatalf("status = %q before 开始, want %q", got, StatusPending)
	}
	if s := r.Stats(); s.Started {
		t.Error("Stats reports the queue as started before 开始 was pressed")
	}

	// A pause before starting changes nothing, and 继续 must not be what launches it.
	r.Pause()
	if s := r.Stats(); s.Started {
		t.Error("暂停 started the queue")
	}
	r.Resume()
	time.Sleep(100 * time.Millisecond)
	if got := r.Jobs()[0].Status; got != StatusPending {
		t.Fatalf("暂停 / 继续 changed the job status to %q", got)
	}

	r.Start()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.Jobs()[0].Status != StatusPending {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := r.Jobs()[0].Status; got == StatusPending {
		t.Error("开始 did not release the job")
	}
	if s := r.Stats(); !s.Started || s.Paused {
		t.Errorf("after 开始: started=%v paused=%v", s.Started, s.Paused)
	}
}

// 暂停 must actually hold the queue, and 继续 must release it. This is only
// testable now that the pool waits for 开始: before, a job added while paused was
// taken by a worker the moment the pause was lifted, so "paused" and "not started"
// were indistinguishable from the outside.
func TestPauseHoldsQueueThenResumes(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	defer r.Shutdown()
	r.EnsureWorkers(1)
	r.Start()

	if !r.TogglePause() {
		t.Fatal("暂停 did not take effect")
	}

	// A file queued while paused must stay pending: the pause is checked on the way
	// out of take(), which is the only place a job can be handed to a worker.
	paused := filepath.Join(dir, "paused.mp4")
	if err := os.WriteFile(paused, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.AddInputs([]InputItem{{Path: paused}}, "t1", "x"); n != 1 {
		t.Fatal("the file was not queued")
	}
	time.Sleep(200 * time.Millisecond)
	for _, j := range r.Jobs() {
		if j.Status == StatusRunning || j.Status == StatusPreparing {
			t.Fatalf("job %s reached %q while the queue was paused", j.ID, j.Status)
		}
	}

	if r.TogglePause() {
		t.Error("继续 left the queue paused")
	}
	if s := r.Stats(); s.Paused || !s.Started {
		t.Errorf("after 继续: paused=%v started=%v", s.Paused, s.Started)
	}

	// 停止 puts the queue back to its initial state, so a later 开始 means "go"
	// rather than "resume something the user never launched".
	r.CancelAll()
	if s := r.Stats(); s.Started {
		t.Error("停止 left the queue armed")
	}
	fresh := filepath.Join(dir, "fresh.mp4")
	if err := os.WriteFile(fresh, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.AddInputs([]InputItem{{Path: fresh}}, "t1", "x"); n != 1 {
		t.Fatal("the second file was not queued")
	}
	time.Sleep(200 * time.Millisecond)
	for _, j := range r.Jobs() {
		if j.Input == fresh && (j.Status == StatusRunning || j.Status == StatusPreparing) {
			t.Errorf("a file added after 停止 started on its own (%q)", j.Status)
		}
	}
}
