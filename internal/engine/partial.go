package engine

import (
	"os"
	"path/filepath"
)

// partialSuffix names the sidecar that says "an encode writing to this exact output
// path was interrupted".
//
// Why a file and not an in-memory set: the case that matters is the user pausing a
// long encode, quitting the app, and re-adding the file next week. By then every
// scrap of runner state is gone, and the only thing left on disk is the truncated
// output. It is usually tens of megabytes -- far from the zero bytes that
// handleProcessed already treats as a leftover -- so without a marker the
// 「已处理过的源文件」 section would look at it, decide the file had been done, and
// skip the very re-run the user asked for. The failure mode is silent and looks
// like the app ignoring them.
const partialSuffix = ".ffmpeggui-part"

// partialMarker is the sidecar path for an output.
func partialMarker(out string) string { return out + partialSuffix }

// markPartial records that out is being written right now. A failure here is not
// worth reporting: ffmpeg has already started and its output is the real artifact,
// the sidecar only guards a later decision.
func markPartial(out string) {
	if out == "" {
		return
	}
	_ = os.WriteFile(partialMarker(out), []byte("1"), 0o644)
}

// clearPartial removes the sidecar, called once the output is final.
func clearPartial(out string) {
	if out == "" {
		return
	}
	_ = os.Remove(partialMarker(out))
}

// isPartial reports whether out is a leftover from an interrupted run.
//
// The path is normalised before comparison because the same output reached through
// a different spelling ("D:\a\..\a\x.mp4" or a trailing separator) is still the
// same file, and a marker that fails to match would silently re-encode a finished
// file -- wasteful, but worse, it would also delete a perfectly good result.
func isPartial(out string) bool {
	marker := partialMarker(out)
	if _, err := os.Stat(marker); err == nil {
		return true
	}
	// The marker is not at that exact path. Walk up to two levels of redundancy
	// rather than trusting one spelling: ResolveOutput builds paths from the
	// destination rule, and the source may itself have come from a relative or
	// un-normalised root.
	if alt := altMarker(out); alt != "" {
		if _, err := os.Stat(alt); err == nil {
			return true
		}
	}
	return false
}

// altMarker looks for the marker next to the fully-resolved path.
func altMarker(out string) string {
	abs, err := filepath.Abs(out)
	if err != nil {
		return ""
	}
	alt := partialMarker(abs)
	if alt == partialMarker(out) {
		return ""
	}
	return alt
}

// existsNonEmpty is the "there is a real result here" test, split out because
// handleProcessed and the tests both need exactly that one meaning.
//
// It doubles as the reason no pruner for stale sidecars is needed: a marker whose
// output has been deleted can never be misread, because every caller checks this
// first and bails out before reaching isPartial. The leftover file is inert.
func existsNonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}
