package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newDeleteRunner is a runner with no ffmpeg behind it: deleting an output only
// reads job fields and touches the filesystem.
func newDeleteRunner() *Runner {
	r := NewRunner()
	r.Configure(Providers{}, nil, nil)
	return r
}

// queueFile puts a real source file in the queue through AddInputs, so the job is
// in the runner's index the way a queued file is. DeleteOutputs looks jobs up by
// id, and a hand-built job would not be found at all.
func queueFile(t *testing.T, r *Runner, dir, name string) *Job {
	t.Helper()
	src := filepath.Join(dir, name)
	writeFile(t, src, "source")
	if n, errs := r.AddInputs([]InputItem{{Path: src}}, "t1", "模板"); n != 1 {
		t.Fatalf("AddInputs added %d jobs (errs=%v), want 1", n, errs)
	}
	return r.jobs[len(r.jobs)-1]
}

// 删除 removes the file and leaves the row: the row is still the record of which
// source this was and what it cost, and the whole point of a separate button is
// that deleting the result is not the same decision as forgetting the job.
func TestDeleteOutputRemovesFileAndKeepsRow(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	job := queueFile(t, r, dir, "a.mp4")
	out := filepath.Join(dir, "a_out.mp4")
	writeFile(t, out, "encoded")
	// The 「已处理过的文件」note from the run that produced this file. It is a
	// claim that a result exists, so it must not outlive the result.
	r.noteProcessed(job.Input, job.TemplateID)
	job.Output, job.Status = out, StatusDone

	res := r.DeleteOutput(job.ID)
	if res.Deleted != 1 || len(res.Errors) != 0 {
		t.Fatalf("DeleteOutput = %+v, want one deleted file and no errors", res)
	}
	if existsAt(t, out) {
		t.Error("the output file is still on disk")
	}
	if r.alreadyProcessed(job.Input, job.TemplateID) {
		t.Error("the record outlived the result it describes")
	}
	if !existsAt(t, job.Input) {
		t.Error("the source file was disturbed")
	}

	jobs := r.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("queue holds %d jobs, want 1 -- the row must stay", len(jobs))
	}
	if !jobs[0].OutputDeleted {
		t.Error("the row does not know its output is gone")
	}
}

// The output was already gone when 删除 was pressed. There is no file to unlink,
// which is a skip and not a failure -- but the record still has to go: it
// describes a result that does not exist, and honouring it would skip a source
// that now has to be encoded again.
func TestDeleteOutputForgetsRecordOfAMissingFile(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	job := queueFile(t, r, dir, "a.mp4")
	job.Output, job.Status = filepath.Join(dir, "a_out.mp4"), StatusDone
	r.noteProcessed(job.Input, job.TemplateID)

	if res := r.DeleteOutput(job.ID); res.Deleted != 0 || res.Skipped != 1 || len(res.Errors) != 0 {
		t.Fatalf("DeleteOutput = %+v, want one skip and no errors", res)
	}
	if r.alreadyProcessed(job.Input, job.TemplateID) {
		t.Error("a record about a file that is not there must be dropped")
	}
}

// A job an encoder is still writing must be refused, with a reason the user can
// act on. The output is held open and still growing; removing it midway leaves
// ffmpeg appending to a path nothing can name any more.
func TestDeleteOutputRefusesRunningJob(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	job := queueFile(t, r, dir, "b.mp4")
	out := filepath.Join(dir, "b_out.mp4")
	writeFile(t, out, "still growing")
	job.Output, job.Status = out, StatusRunning

	res := r.DeleteOutput(job.ID)
	if res.Deleted != 0 {
		t.Fatalf("deleted a file an encoder is still writing: %+v", res)
	}
	if !existsAt(t, out) {
		t.Error("a running job's output must be left alone")
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "正在处理中") {
		t.Errorf("errors = %v, want one 正在处理中 refusal", res.Errors)
	}
}

// The one guard that matters most: 删除输出 must never become 删除素材. A template
// can legitimately write next to its input, and the source is the irreplaceable
// half of the pair.
func TestDeleteOutputNeverDeletesTheSource(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	job := queueFile(t, r, dir, "c.mp4")
	job.Output, job.Status = job.Input, StatusDone

	res := r.DeleteOutput(job.ID)
	if res.Deleted != 0 {
		t.Fatalf("deleted the source file: %+v", res)
	}
	if !existsAt(t, job.Input) {
		t.Fatal("the source file is gone")
	}
	if len(res.Errors) != 1 {
		t.Errorf("errors = %v, want one refusal", res.Errors)
	}
}

// Everything with nothing to delete is a skip, not a failure: the caller asked for
// the file not to be there, and it is not there. Unknown ids are skipped too --
// they come from rows that can be removed while the batch is being built.
func TestDeleteOutputsSkipsWhatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	a := queueFile(t, r, dir, "a.mp4")
	b := queueFile(t, r, dir, "b.mp4")
	c := queueFile(t, r, dir, "c.mp4")

	aOut := filepath.Join(dir, "a_out.mp4")
	writeFile(t, aOut, "encoded")
	a.Output, a.Status = aOut, StatusDone
	b.Output, b.Status = filepath.Join(dir, "b_out.mp4"), StatusDone // never written
	c.Status = StatusPending                                         // no output decided yet

	res := r.DeleteOutputs([]string{a.ID, b.ID, c.ID, "no-such-id", "  "})
	if res.Deleted != 1 || len(res.Paths) != 1 || res.Paths[0] != aOut {
		t.Fatalf("DeleteOutputs = %+v, want exactly a_out.mp4 deleted", res)
	}
	if res.Skipped != 3 {
		t.Errorf("skipped = %d, want 3 (already gone / no output / unknown id)", res.Skipped)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors = %v, want none -- none of these is a failure", res.Errors)
	}
	if existsAt(t, aOut) {
		t.Error("a_out.mp4 survived the batch")
	}
}

// A path that is a directory is refused rather than recursed into: os.Remove drops
// an empty directory, and the output of a job is never supposed to be one.
func TestDeleteOutputRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	r := newDeleteRunner()
	job := queueFile(t, r, dir, "d.mp4")
	out := filepath.Join(dir, "d_out.mp4")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	job.Output, job.Status = out, StatusDone

	res := r.DeleteOutput(job.ID)
	if res.Deleted != 0 || len(res.Errors) != 1 {
		t.Fatalf("DeleteOutput = %+v, want one refusal and nothing deleted", res)
	}
	if !existsAt(t, out) {
		t.Error("the directory was removed")
	}
}
