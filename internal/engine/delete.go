package engine

import (
	"fmt"
	"os"
	"strings"
)

// DeleteResult reports what a 删除 did, one job at a time.
//
// Deleted counts files that were actually unlinked. Skipped counts jobs that had
// nothing to unlink -- no output decided yet, or the file was already gone --
// because "there is no file there" is the state the caller asked for, not a
// failure. Errors carries one readable line per job that really could not be
// handled: a busy job, a directory where a file was expected, an unlink Windows
// refused.
type DeleteResult struct {
	Deleted int      `json:"deleted"`
	Skipped int      `json:"skipped"`
	Paths   []string `json:"paths"`
	Errors  []string `json:"errors"`
}

// DeleteOutput removes the file one job produced.
//
// This is deliberately not RemoveJob. The row is not the thing being deleted --
// it is the only place that still says which source this was, how long it took
// and which command made it, and deleting an output is usually the step before
// running it again. So the queue keeps its line, the disk loses the file, and
// OutputDeleted is what lets the row admit the file is gone instead of offering
// to open something that is not there.
//
// The file is the only thing on disk that is touched. The source, the template
// and the history record all stay: 记录 is the log of what happened, and
// rewriting it to match a later decision about the disk would make it useless as
// a record. The one piece of state that does go is the in-session 「已处理过的
// 文件」note, because it is a claim that a result exists.
func (r *Runner) DeleteOutput(id string) DeleteResult {
	return r.DeleteOutputs([]string{id})
}

// DeleteOutputs deletes the outputs of several jobs.
//
// One job at a time, each under its own job lock: the checks and the unlink are
// per-file work and there is nothing to gain by holding them together -- unlike
// RemoveJobs, where the point of the batch lock was to emit one stats event
// instead of fifty.
func (r *Runner) DeleteOutputs(ids []string) DeleteResult {
	res := DeleteResult{Paths: []string{}, Errors: []string{}}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// The runner lock is only ever taken to find the job, never held across the
		// unlink: an OS call under the queue lock would stall every worker that is
		// trying to report progress.
		r.mu.Lock()
		j := r.index[id]
		r.mu.Unlock()
		if j == nil {
			res.Skipped++
			continue
		}
		path, deleted, err := r.deleteJobOutput(j)
		switch {
		case err != nil:
			res.Errors = append(res.Errors, err.Error())
		case deleted:
			res.Deleted++
			res.Paths = append(res.Paths, path)
		default:
			res.Skipped++
		}
	}
	return res
}

// deleteJobOutput is the per-file half: decide, unlink, and write down that it
// happened. deleted is false when there was nothing to remove, which is not an
// error -- the point of the call is that the file should not be there.
func (r *Runner) deleteJobOutput(j *Job) (path string, deleted bool, err error) {
	j.lock()
	status := j.Status
	out := strings.TrimSpace(j.Output)
	// input and tplID identify the 「已处理过的文件」record, so both are read here
	// rather than after the unlock: tplID is the one mutable half of that pair.
	input, tplID := j.Input, j.TemplateID
	j.unlock()

	// A running job's output is held open by ffmpeg and still growing. Removing it
	// midway leaves the encoder appending to a path nothing can name any more, and
	// on Windows the unlink usually fails outright. Refusing with a reason the user
	// can act on beats a file that half disappears.
	if status == StatusRunning || status == StatusPreparing {
		return "", false, fmt.Errorf("%s 正在处理中，请先暂停或移除任务，再删除输出文件", j.InputName)
	}
	if out == "" {
		return "", false, nil
	}
	// ResolveOutput already refuses to name the source as its own output, but this
	// is the one code path that destroys a file: the guard belongs where the delete
	// happens, not only where the name was chosen.
	if input != "" && samePath(out, input) {
		return "", false, fmt.Errorf("%s 的输出路径就是源文件，已跳过", j.InputName)
	}

	st, err := os.Stat(out)
	if err != nil {
		if os.IsNotExist(err) {
			// Gone already -- deleted outside the app, or never written. The
			// record is a statement about a result, and there is no result, so
			// it goes too: keeping it would skip a source that has to be
			// encoded again.
			r.dropProcessed(input, tplID)
			return out, false, nil
		}
		return "", false, err
	}
	if st.IsDir() {
		return "", false, fmt.Errorf("%s 的输出路径是一个目录，没有删除", out)
	}
	if err := os.Remove(out); err != nil {
		return "", false, err
	}
	// Same reasoning as above: the record described the file that was just
	// removed. Letting it stand would make the next pass skip a file whose
	// result no longer exists anywhere.
	r.dropProcessed(input, tplID)

	j.lock()
	j.OutputDeleted = true
	j.unlock()
	// logLine also emits the job, so the row picks up OutputDeleted and the log
	// keeps its own record of the delete next to the run that produced the file.
	r.logLine(j, "已删除输出文件: "+out)
	return out, true, nil
}
