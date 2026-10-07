package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

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

// runQueue queues items, starts the runner and waits for the queue to drain.
// It is the whole-vs-unit split this file needs: the policy under test here acts
// on real files during a real run, not just in handleProcessed.
func runQueue(t *testing.T, r *Runner, items []InputItem, tpl store.Template) []Job {
	t.Helper()
	if n, errs := r.AddInputs(items, tpl.ID, tpl.Name); n != len(items) {
		t.Fatalf("queued %d of %d jobs (errors: %v)", n, len(items), errs)
	}
	r.Start()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if st := r.Stats(); st.Pending == 0 && st.Running == 0 {
			return r.Jobs()
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("the queue never drained")
	return nil
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
// job is skipped.
func TestHandleProcessedKeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "old result")

	r, job := newPolicyRunner(t, src, dir)
	r.noteProcessed(src, job.TemplateID)
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

// The record is the only trigger, and it is a record of *this app having encoded
// this file* -- never of something merely being present at the output path.
//
// Every state a file could be in sits there in turn: a plausible complete
// result, a zero-byte leftover, a truncated corpse from an interrupted encode, a
// path where nothing exists at all. None of them is a record, so none of them
// skips.
func TestHandleProcessedIgnoresWhateverIsOnDisk(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	tpl := store.Template{Existing: &store.ExistingSpec{}}
	writeFile(t, src, "source")

	states := []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{"a file that looks finished", func(t *testing.T, p string) {
			writeFile(t, p, "hand made, or from another tool")
		}},
		{"a zero-byte leftover", func(t *testing.T, p string) {
			writeFile(t, p, "")
		}},
		{"a truncated encode from an interrupted run", func(t *testing.T, p string) {
			writeFile(t, p, "0123456789partial encode that never finished")
		}},
		{"nothing at all", func(t *testing.T, p string) {}},
	}
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			_ = os.Remove(out)
			st.write(t, out)

			r, job := newPolicyRunner(t, src, dir)
			if stop := r.handleProcessed(job, store.Settings{}, tpl, out); stop {
				t.Errorf("%s was taken for a file this app has processed", st.name)
			}
			if job.Status == StatusSkipped {
				t.Error("job was skipped without a record")
			}
		})
	}

	// And the reverse: a record is enough on its own, even with nothing on disk.
	// The question the section asks is about the source, not about the result.
	missing := filepath.Join(dir, "gone_out.mp4")
	r, job := newPolicyRunner(t, src, dir)
	r.noteProcessed(src, job.TemplateID)
	if stop := r.handleProcessed(job, store.Settings{}, tpl, missing); !stop {
		t.Error("a recorded run must be honoured even if the result was moved away")
	}
}

// A record is about one file under one template. Neither half may leak: a
// different file is not covered by it, and neither is the same file through a
// different template -- that template has produced nothing yet.
func TestProcessedRecordIsPerFileAndPerTemplate(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, a, "source")
	writeFile(t, b, "source")
	tpl := store.Template{Existing: &store.ExistingSpec{}}

	r, jobA := newPolicyRunner(t, a, dir)
	r.noteProcessed(a, "t1")

	if stop := r.handleProcessed(jobA, store.Settings{}, tpl, out); !stop {
		t.Error("the same file under the same template must skip")
	}
	// The path is compared the way the queue compares it, so a difference in
	// case is still the same file.
	if !r.alreadyProcessed(strings.ToUpper(a), "t1") {
		t.Error("case must not make it a different file")
	}
	if r.alreadyProcessed(b, "t1") {
		t.Error("one file's record must not cover another file")
	}
	if r.alreadyProcessed(a, "t2") {
		t.Error("one template's record must not cover another template")
	}
}

// The record lives in the Runner and nowhere else, so it ends with the process.
// This is the promise the section makes: handle a folder today, open the app
// tomorrow, and every file is encoded again instead of being silently skipped.
func TestProcessedRecordDiesWithTheRunner(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "result")

	r, job := newPolicyRunner(t, src, dir)
	r.noteProcessed(src, job.TemplateID)

	// A record that only exists in memory leaves nothing at all beside anyone's
	// media -- which is the whole point of it not being a file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if got := strings.Join(names, ", "); got != "in.mp4, in_out.mp4" {
		t.Errorf("processing left %q in the folder; want only the two files", got)
	}

	// Same file, same template, a new run of the app: nothing remembers, so the
	// file is encoded again rather than reported as already handled.
	fresh, job2 := newPolicyRunner(t, src, dir)
	if stop := fresh.handleProcessed(job2, store.Settings{}, store.Template{Existing: &store.ExistingSpec{}}, out); stop {
		t.Error("a new session must process the file again")
	}
}

// Forgetting is deliberate and always available: 删除输出文件 drops one record,
// saving a template drops that template's, and saving 基本配置 drops them all --
// a record is only ever true of the settings it was made with.
func TestProcessedRecordCanBeForgotten(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "in_out.mp4")
	writeFile(t, src, "source")
	writeFile(t, out, "result")

	t.Run("one file", func(t *testing.T) {
		r, job := newPolicyRunner(t, src, dir)
		r.noteProcessed(src, "t1")
		r.dropProcessed(src, "t1")
		if r.alreadyProcessed(src, "t1") {
			t.Error("dropProcessed left the record behind")
		}
		if stop := r.handleProcessed(job, store.Settings{}, store.Template{Existing: &store.ExistingSpec{}}, out); stop {
			t.Error("a forgotten file must be processed again")
		}
	})

	t.Run("one template", func(t *testing.T) {
		r, _ := newPolicyRunner(t, src, dir)
		r.noteProcessed(src, "t1")
		r.noteProcessed(src, "t2")
		r.ForgetTemplate("t1")
		if r.alreadyProcessed(src, "t1") {
			t.Error("ForgetTemplate left its own records behind")
		}
		if !r.alreadyProcessed(src, "t2") {
			t.Error("ForgetTemplate reached into another template")
		}
	})

	t.Run("everything", func(t *testing.T) {
		r, _ := newPolicyRunner(t, src, dir)
		r.noteProcessed(src, "t1")
		r.noteProcessed(src, "t2")
		r.ForgetAllProcessed()
		if r.alreadyProcessed(src, "t1") || r.alreadyProcessed(src, "t2") {
			t.Error("ForgetAllProcessed left records behind")
		}
	})
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
	r.noteProcessed(src, job.TemplateID)
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
			r.noteProcessed(src, job.TemplateID)
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
	r.noteProcessed(src, job.TemplateID)
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
	r.noteProcessed(src, job.TemplateID)
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
	r.noteProcessed(src, job.TemplateID)
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

// The other half of the policy: a run that has just produced a verified output
// files its own source away. This is the part that never used to happen at all
// -- the action only ran on the skip path, so 「移动到目标目录」 changed nothing
// on a first pass and looked like a dead setting.
func TestFileProcessedSourceMovesOnFirstPass(t *testing.T) {
	for _, action := range []string{store.ActionMove, store.ActionCopy} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "b.mp4")
			writeFile(t, src, "source")

			r, job := newPolicyRunner(t, src, root)
			tpl := store.Template{Name: "模板", Existing: &store.ExistingSpec{
				Action: action,
				Dest:   store.DestRule{Mode: store.OutputSibling, Suffix: "_done"},
			}}
			r.fileProcessedSource(job, store.Settings{}, tpl)

			archived := root + "_done" + string(filepath.Separator) + "b.mp4"
			if !existsAt(t, archived) {
				t.Fatalf("the source was not filed away; warnings = %v", job.Warnings)
			}
			if body, _ := os.ReadFile(archived); string(body) != "source" {
				t.Errorf("relocated contents = %q, want %q", body, "source")
			}
			// move takes the source away, copy leaves it where it was.
			wantOrig := action == store.ActionCopy
			if got := existsAt(t, src); got != wantOrig {
				t.Errorf("source still exists = %v, want %v", got, wantOrig)
			}
			if len(job.Warnings) != 0 {
				t.Errorf("filing the source away is not a warning: %v", job.Warnings)
			}
		})
	}
}

// A section that says 留在原处 (or has no action at all) must not touch the source.
func TestFileProcessedSourceKeepsWhenNotAsked(t *testing.T) {
	for _, ex := range []*store.ExistingSpec{
		nil,
		{},
		{Action: store.ActionKeep},
	} {
		root := t.TempDir()
		src := filepath.Join(root, "b.mp4")
		writeFile(t, src, "source")

		r, job := newPolicyRunner(t, src, root)
		r.fileProcessedSource(job, store.Settings{}, store.Template{Existing: ex})

		if !existsAt(t, src) {
			t.Errorf("section %+v moved the source", ex)
		}
		if len(job.Warnings) != 0 {
			t.Errorf("section %+v produced warnings: %v", ex, job.Warnings)
		}
	}
}

// A destination that cannot be used is reported on the row: the encode did work,
// so the job stays a success, but the user asked for the source to be filed away
// and it was not.
func TestFileProcessedSourceWarnsOnBadDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "b.mp4")
	writeFile(t, src, "source")

	r, job := newPolicyRunner(t, src, root)
	tpl := store.Template{Name: "模板", Existing: &store.ExistingSpec{
		Action: store.ActionMove,
		Dest:   store.DestRule{Mode: store.OutputMirror}, // no directory
	}}
	r.fileProcessedSource(job, store.Settings{}, tpl)

	if !existsAt(t, src) {
		t.Error("the source must be left alone when it cannot be relocated")
	}
	if len(job.Warnings) == 0 {
		t.Error("a source that could not be filed away has to be reported")
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

// The section end to end, on real files: a first pass over a folder must encode
// the file AND file its source away, and the same run of the app must then
// report the file as 已跳过 instead of encoding it a second time.
//
// The move is the half that never used to run at all -- it was reachable only
// from the skip path, and a skip needs a record a first pass cannot have -- so
// this is the regression that matters: set 「移动到目标目录」 and the source
// actually leaves.
func TestExistingSectionMovesSourceOnFirstPass(t *testing.T) {
	bins := ffmpegBinaries(t)

	work := t.TempDir()
	store.SetDataDir(filepath.Join(work, "data"))
	inDir := filepath.Join(work, "in")
	doneDir := filepath.Join(work, "done")
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The source lives one level down so the "同级目录 + 后缀" rule has a root to
	// work from, and the archive directory is given explicitly.
	src := filepath.Join(inDir, "clip.mp4")
	makeClip(t, bins, src, 320, 180, 1, "120k")

	settings := store.DefaultSettings()
	settings.PreventSleep = false

	global := store.DefaultGlobalTemplate()
	global.OutMode = store.OutputSibling
	global.Perf.Concurrency = 1
	global.Perf.LogLevel = "warning"

	tpl := store.Template{
		ID: "t1", Name: "copy", Container: "",
		VideoMode: store.ModeCopy, AudioMode: store.ModeCopy,
		Existing: &store.ExistingSpec{
			Action: store.ActionMove,
			Dest:   store.DestRule{Mode: store.OutputCustom, Dir: doneDir},
		},
	}
	tpl.Normalize()

	providers := Providers{
		Settings:       func() store.Settings { return settings },
		Binaries:       func() media.Binaries { return bins },
		Template:       func(id string) (store.Template, bool) { return tpl, id == "t1" },
		GlobalTemplate: func() store.Template { return global },
	}

	r := NewRunner()
	r.Configure(providers, nil, nil)
	jobs := runQueue(t, r, []InputItem{{Path: src}}, tpl)
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	first := jobs[0]
	if first.Status != StatusDone && first.Status != StatusWarning {
		t.Fatalf("first pass ended as %s (error: %s)", first.Status, first.Error)
	}
	if _, err := os.Stat(first.Output); err != nil {
		t.Fatalf("no output produced: %v", err)
	}
	if !r.alreadyProcessed(first.Input, first.TemplateID) {
		t.Error("a verified run has to be noted as processed")
	}
	// The source left, and it landed in the archive with the same name.
	if existsAt(t, src) {
		t.Error("the source was not moved away on the first pass")
	}
	archived := filepath.Join(doneDir, "clip.mp4")
	if !existsAt(t, archived) {
		t.Fatalf("the source did not arrive in %s (warnings: %v)", doneDir, first.Warnings)
	}
	if st, err := os.Stat(archived); err != nil || st.Size() == 0 {
		t.Errorf("the archived source is empty: %v", err)
	}

	// The same file, added again in the same session. The row has to go first --
	// the queue refuses to hold the same path twice -- and then the note left by
	// the run above must make this one a skip rather than another encode, with
	// the source filed away again.
	body, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveJob(first.ID); err != nil {
		t.Fatal(err)
	}

	jobs2 := runQueue(t, r, []InputItem{{Path: src}}, tpl)
	if len(jobs2) != 1 {
		t.Fatalf("expected one job on the second pass, got %d", len(jobs2))
	}
	if jobs2[0].Status != StatusSkipped {
		t.Fatalf("second pass = %s, want skipped (message: %s)", jobs2[0].Status, jobs2[0].Message)
	}
	if existsAt(t, src) {
		t.Error("the skip path must file the source away too")
	}
	// The first relocation is still where it was; this one had to step aside
	// rather than run over it.
	if !existsAt(t, filepath.Join(doneDir, "clip_1.mp4")) {
		t.Error("the second relocation should have been numbered instead of clobbering the first")
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
