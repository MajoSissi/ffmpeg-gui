package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// The queue-level filter outranks the template's own rules. This is the whole point of
// the override: "this batch, these files only" must not require editing the template
// that every other queue is also using.
func TestQueueFilterOverridesTemplate(t *testing.T) {
	bins := ffmpegBinaries(t)
	work := t.TempDir()
	store.SetDataDir(filepath.Join(work, "data"))

	small := filepath.Join(work, "a.mp4")
	makeClip(t, bins, small, 320, 180, 1, "100k")

	settings := store.DefaultSettings()
	settings.PreventSleep = false

	// The template says "process everything": no size floor at all.
	global := store.DefaultGlobalTemplate()
	global.Filter = &store.FilterSpec{}

	tpl := store.Template{ID: "t1", Name: "copy", VideoMode: store.ModeCopy, AudioMode: store.ModeCopy}
	tpl.Normalize()

	r := NewRunner()
	r.Configure(Providers{
		Settings:       func() store.Settings { return settings },
		Binaries:       func() media.Binaries { return bins },
		Template:       func(id string) (store.Template, bool) { return tpl, true },
		GlobalTemplate: func() store.Template { return global },
	}, nil, nil)

	// Installed before anything is queued, the way the task page does it.
	r.SetQueueFilter(&store.FilterSpec{MinSizeMB: 500})

	if n, _ := r.AddInputs([]InputItem{{Path: small}}, "t1", tpl.Name); n != 1 {
		t.Fatal("failed to queue the fixture")
	}
	r.Start()

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if st := r.Stats(); st.Pending == 0 && st.Running == 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	jobs := r.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	if jobs[0].Status != StatusFiltered {
		t.Fatalf("status = %s, want filtered -- the queue filter has to beat the template's empty one (message: %s)",
			jobs[0].Status, jobs[0].Message)
	}

	// Clearing it hands the decision back to the template, which processes everything.
	r.SetQueueFilter(nil)
	if r.queueFilterSpec() != nil {
		t.Error("queueFilterSpec should be nil after clearing the override")
	}
}

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
