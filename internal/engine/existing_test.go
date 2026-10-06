package engine

import (
	"os"
	"path/filepath"
	"testing"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// writeFile creates path (and its parents) with the given contents.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func existsAt(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// A section that only touches the filesystem and the job's own fields, so a
// runner with no ffmpeg wired up is enough. sourceRoot must be a real directory:
// relocation resolves against it, and a relative placeholder makes
// ResolveDestDir write into the package directory instead of the temp dir.
func newPolicyRunner(t *testing.T, input, sourceRoot string) (*Runner, *Job) {
	t.Helper()
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	return r, NewJob(input, sourceRoot, "t1", "模板")
}

// 「留在原处」is the whole policy in miniature: the source is not touched and the
// job is skipped. It must only fire for a real previous result -- a zero-byte
// leftover is what an interrupted run leaves behind, and treating that as
// finished would skip the retry the user actually wants.
func TestHandleProcessedKeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "old result")

	r, job := newPolicyRunner(t, src, dir)
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
		t.Fatal("an already-processed file must not be encoded again")
	}
	if job.Status != StatusSkipped {
		t.Errorf("status = %q, want %q", job.Status, StatusSkipped)
	}
	// Both files stay where they were: the policy keeps the source, and the
	// finished result is not this function's business.
	if !existsAt(t, src) {
		t.Error("the source file was removed")
	}
	if body, _ := os.ReadFile(out); string(body) != "old result" {
		t.Errorf("the previous result was disturbed: %q", body)
	}
}

// A zero-byte leftover is not a finished result, and nothing at all is not
// either. Both must let the pipeline carry on.
func TestHandleProcessedIgnoresUnfinishedOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	tpl := store.Template{Existing: &store.ExistingSpec{}}

	writeFile(t, out, "")
	r, job := newPolicyRunner(t, src, dir)
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); stop {
		t.Error("a zero-byte leftover must not count as already processed")
	}

	r2, job2 := newPolicyRunner(t, src, dir)
	if stop := r2.handleProcessed(job2, store.Settings{}, tpl, filepath.Join(dir, "nope.mp4")); stop {
		t.Error("a missing output must not count as already processed")
	}
}

// A nil section is the "follow the global template" state and must do nothing at
// all -- not even skip.
func TestHandleProcessedNilSection(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "old result")

	r, job := newPolicyRunner(t, src, dir)
	if stop := r.handleProcessed(job, store.Settings{}, store.Template{}, out); stop {
		t.Error("a nil section must not end the job")
	}
	if !existsAt(t, src) {
		t.Error("a nil section must not touch the source")
	}
}

// 移动 / 复制 act on the SOURCE, which is the point of the section: the aim is to
// stop the source tree from feeding the same file back in, so the source is what
// moves. move takes it away; copy leaves it to be overwritten.
func TestHandleProcessedRelocatesSource(t *testing.T) {
	cases := []struct {
		action string
		// suffix is what the rule appends; "" means "mirror into dir"
		suffix string
		// mirror sends the whole tree into one directory instead
		mirror bool
		// whether the original path must still exist afterwards
		orig bool
	}{
		{action: store.ActionMove, suffix: "_done"},
		{action: store.ActionCopy, suffix: "_done", orig: true},
		{action: store.ActionMove, mirror: true},
	}
	for _, tc := range cases {
		name := tc.action + "/sibling"
		if tc.mirror {
			name = tc.action + "/mirror"
		}
		t.Run(name, func(t *testing.T) {
			// mmd/sub/b.mp4 was already processed, so its result is sitting in
			// the source tree's own output folder.
			root := t.TempDir()
			src := filepath.Join(root, "sub", "b.mp4")
			out := filepath.Join(root, "out", "sub", "b.mp4")
			writeFile(t, src, "source")
			writeFile(t, out, "old result")

			rule := store.DestRule{Mode: store.OutputSibling, Suffix: tc.suffix}
			archived := root + "_done" + string(filepath.Separator) + filepath.Join("sub", "b.mp4")
			if tc.mirror {
				dir := filepath.Join(t.TempDir(), "done")
				rule = store.DestRule{Mode: store.OutputMirror, Dir: dir}
				archived = filepath.Join(dir, "sub", "b.mp4")
			}

			r, job := newPolicyRunner(t, src, root)
			tpl := store.Template{
				Existing: &store.ExistingSpec{Action: tc.action, Dest: rule},
			}
			if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
				t.Fatalf("%q must end the job", tc.action)
			}
			if job.Status != StatusSkipped {
				t.Errorf("status = %q, want %q", job.Status, StatusSkipped)
			}
			if got := existsAt(t, src); got != tc.orig {
				t.Errorf("source still exists = %v, want %v", got, tc.orig)
			}
			if !existsAt(t, archived) {
				t.Fatalf("the source was not relocated to %s", archived)
			}
			if body, _ := os.ReadFile(archived); string(body) != "source" {
				t.Errorf("relocated contents = %q, want %q", body, "source")
			}
			// The finished result is deliberately untouched: this policy is about
			// the source, not about what the previous pass produced.
			if body, _ := os.ReadFile(out); string(body) != "old result" {
				t.Errorf("the previous result was disturbed: %q", body)
			}
		})
	}
}

// The rename template applies to the relocated source.
func TestHandleProcessedRenameTemplate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "b.mp4")
	out := filepath.Join(root, "out", "b.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "old result")

	r, job := newPolicyRunner(t, src, root)
	tpl := store.Template{Existing: &store.ExistingSpec{
		Action:  store.ActionMove,
		Dest:    store.DestRule{Mode: store.OutputSibling, Suffix: "_done"},
		Pattern: "{name}_v1.{ext}",
	}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
		t.Fatal("move must end the job")
	}
	if !existsAt(t, root+"_done"+string(filepath.Separator)+"b_v1.mp4") {
		t.Errorf("the rename template was not applied; got %v", job.Warnings)
	}
}

// Two passes over the same folder meet the same destination. The first relocated
// source must survive: clobbering it is how a rerun silently destroys the earlier
// pass, so the default has to append _1.
func TestHandleProcessedRelocationDoesNotClobber(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "b.mp4")
	out := filepath.Join(root, "out", "b.mp4")
	tpl := store.Template{Existing: &store.ExistingSpec{
		Action: store.ActionMove,
		Dest:   store.DestRule{Mode: store.OutputSibling, Suffix: "_done"},
	}}
	first := root + "_done" + string(filepath.Separator) + "b.mp4"

	writeFile(t, src, "source")
	writeFile(t, out, "old result")
	writeFile(t, first, "older pass")

	r, job := newPolicyRunner(t, src, root)
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
		t.Fatal("move must end the job")
	}
	if body, _ := os.ReadFile(first); string(body) != "older pass" {
		t.Errorf("the earlier relocated file was overwritten: %q", body)
	}
	if !existsAt(t, root+"_done"+string(filepath.Separator)+"b_1.mp4") {
		t.Error("the new copy should have been numbered instead")
	}
}

// A relocation with nowhere to go fails the job and leaves the source alone --
// silently skipping would look like the policy worked.
func TestHandleProcessedWithoutDirFails(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "b.mp4")
	out := filepath.Join(root, "out", "b.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "old result")

	r, job := newPolicyRunner(t, src, root)
	// mirror without a directory is a misconfiguration: Validate rejects it, but
	// a hand-edited template can still arrive here.
	tpl := store.Template{Existing: &store.ExistingSpec{
		Action: store.ActionMove,
		Dest:   store.DestRule{Mode: store.OutputMirror},
	}}
	if stop := r.handleProcessed(job, store.Settings{}, tpl, out); !stop {
		t.Fatal("an unusable destination must end the job")
	}
	if job.Status != StatusFailed || job.Error == "" {
		t.Errorf("expected a failure with a reason, got %q / %q", job.Status, job.Error)
	}
	if !existsAt(t, src) {
		t.Error("the source must be left alone when it cannot be relocated")
	}
}

// ResolveOutput must NOT dodge an occupied path: handing ffmpeg a fresh name
// would encode the same file twice with no trace in the UI. Reacting to the
// collision belongs to the runner, the only place that can also see the source.
func TestResolveOutputKeepsOccupiedPath(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "in")
	src := filepath.Join(dir, "a.mp4")
	writeFile(t, src, "source")
	occupied := filepath.Join(base, "in_out", "a.mp4")
	writeFile(t, occupied, "old result")

	got, err := ResolveOutput(OutputRequest{
		Info: &media.Info{Path: src, Ext: "mp4"},
		Tpl: store.Template{
			OutMode:  store.OutputSibling,
			Existing: &store.ExistingSpec{Action: store.ActionMove},
		},
		SrcRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != occupied {
		t.Errorf("ResolveOutput = %q, want the occupied %q", got, occupied)
	}
}

// OutputRoot has to name the directory ResolveOutput writes into.
func TestOutputRootMatchesResolveOutput(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "sub", "b.mp4")
	info := &media.Info{Path: src, Ext: "mp4"}

	for _, mode := range []string{store.OutputSame, store.OutputSibling, store.OutputMirror} {
		g := store.DefaultGlobalTemplate()
		g.OutMode = mode
		if mode == store.OutputMirror {
			g.OutDir = filepath.Join("D:", "out")
		}
		eff := store.Template{}.Effective(g)
		req := OutputRequest{Info: info, Tpl: eff, SrcRoot: root}

		root1, err := OutputRoot(req)
		if err != nil {
			t.Fatal(err)
		}
		out, err := ResolveOutput(req)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(out) != root1 {
			t.Errorf("%s: ResolveOutput wrote to %s but OutputRoot said %s", mode, filepath.Dir(out), root1)
		}
	}
}
