package media

import (
	"os/exec"
	"testing"
)

// TestEncodersParsesRealFFmpeg exercises the `-encoders` parser against the
// ffmpeg that happens to be installed; it is skipped when none is on PATH.
func TestEncodersParsesRealFFmpeg(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found in PATH")
	}
	have, err := Encoders(ff)
	if err != nil {
		t.Fatalf("Encoders(%q): %v", ff, err)
	}
	if !have["libx264"] {
		t.Errorf("expected libx264 to be reported as available (%d encoders parsed)", len(have))
	}
	if len(have) < 50 {
		t.Errorf("parsed only %d encoders, the flags column is probably mis-detected", len(have))
	}
	if have["="] {
		t.Errorf("legend line leaked into the encoder set")
	}
}

func TestEncodersMissingPath(t *testing.T) {
	if _, err := Encoders(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}
