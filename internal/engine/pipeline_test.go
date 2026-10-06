package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

func TestComputeScaleLongEdge(t *testing.T) {
	cases := []struct {
		name    string
		w, h    int
		spec    store.ResizeSpec
		wantW   int
		wantH   int
		applied bool
	}{
		{
			name: "4K 横屏 -> 长边 2560",
			w:    3840, h: 2160,
			spec:  store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, OnlyLarger: true, MultipleOf: 2},
			wantW: 2560, wantH: 1440, applied: true,
		},
		{
			name: "4K 竖屏 -> 长边 2560（长边是高）",
			w:    2160, h: 3840,
			spec:  store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, OnlyLarger: true, MultipleOf: 2},
			wantW: 1440, wantH: 2560, applied: true,
		},
		{
			name: "1080p 不放大",
			w:    1920, h: 1080,
			spec:    store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, OnlyLarger: true, MultipleOf: 2},
			applied: false,
		},
		{
			name: "奇数列对齐到偶数",
			w:    3840, h: 2161,
			spec:  store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 1000, OnlyLarger: true, MultipleOf: 2},
			wantW: 1000, wantH: 564, applied: true,
		},
		{
			name: "fit 只缩不放",
			w:    1280, h: 720,
			spec:    store.ResizeSpec{Mode: store.ResizeFit, MaxWidth: 1920, MaxHeight: 1080, MultipleOf: 2},
			applied: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &media.Info{
				Path: "x.mp4", Ext: "mp4",
				Width: tc.w, Height: tc.h, DisplayWidth: tc.w, DisplayHeight: tc.h,
				Video: &media.Stream{Codec: "h264"},
			}
			w, h, filter, applied, _ := computeScale(info, tc.spec)
			if applied != tc.applied {
				t.Fatalf("applied = %v, want %v", applied, tc.applied)
			}
			if !applied {
				return
			}
			if w != tc.wantW || h != tc.wantH {
				t.Fatalf("got %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
			if filter == "" {
				t.Fatal("empty filter")
			}
		})
	}
}

func TestComputeScaleHonoursRotation(t *testing.T) {
	// Stored as 1920x1080 but flagged rotate=90: it displays as 1080x1920, so
	// its long edge is 1920. A "long edge 1920" rule must therefore leave it
	// alone, while a "long edge 1080" rule must shrink the long (vertical) side.
	info := &media.Info{
		Path: "x.mp4", Ext: "mp4",
		Width: 1920, Height: 1080, DisplayWidth: 1080, DisplayHeight: 1920, Rotation: 90,
		Video: &media.Stream{Codec: "h264"},
	}

	_, _, _, applied, _ := computeScale(info, store.ResizeSpec{
		Mode: store.ResizeLongEdge, LongEdge: 1920, OnlyLarger: true, MultipleOf: 2,
	})
	if applied {
		t.Error("a portrait 1080x1920 clip is already at the 1920 cap, it must not be touched")
	}

	w, h, _, applied, _ := computeScale(info, store.ResizeSpec{
		Mode: store.ResizeLongEdge, LongEdge: 1080, OnlyLarger: true, MultipleOf: 2,
	})
	if !applied {
		t.Fatal("expected the clip to be scaled down to a 1080 long edge")
	}
	if h != 1080 || w != 608 {
		t.Fatalf("got %dx%d, want 608x1080 (long edge stays vertical)", w, h)
	}
}

func TestEvaluateFiltersSize(t *testing.T) {
	info := &media.Info{
		Path: "small.mp4", Ext: "mp4",
		Size: 18 * 1024 * 1024, Duration: 12,
		Width: 1280, Height: 720, DisplayWidth: 1280, DisplayHeight: 720,
	}
	pass, reason := EvaluateFilters(info, store.FilterSpec{MinSizeMB: 300})
	if pass {
		t.Fatal("expected the small file to be rejected")
	}
	if reason == "" {
		t.Fatal("expected a reason")
	}

	big := &media.Info{Path: "b.mp4", Ext: "mp4", Size: 900 * 1024 * 1024, Duration: 600,
		Width: 3840, Height: 2160, DisplayWidth: 3840, DisplayHeight: 2160}
	if pass, _ := EvaluateFilters(big, store.FilterSpec{MinSizeMB: 300}); !pass {
		t.Fatal("expected the large file to pass")
	}
}

// A spec with every field at zero must be inert: "不限制" has to be
// distinguishable from "没有配置".
func TestEvaluateFiltersEmptySpecPasses(t *testing.T) {
	info := &media.Info{Path: "a.mp4", Ext: "mp4", Size: 1, Duration: 1,
		Width: 320, Height: 180, DisplayWidth: 320, DisplayHeight: 180}
	if pass, reason := EvaluateFilters(info, store.FilterSpec{}); !pass {
		t.Fatalf("an empty filter must let everything through, got %q", reason)
	}
}

func TestResolveOutputAvoidsOverwritingSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	_ = os.WriteFile(src, []byte("x"), 0o644)

	info := &media.Info{Path: src, Ext: "mp4", DisplayWidth: 1920, DisplayHeight: 1080,
		Video: &media.Stream{Codec: "h264"}}
	global := store.DefaultGlobalTemplate()
	global.OutMode = store.OutputSame
	global.OutPattern = "{name}" // deliberately identical to the source name

	out, err := ResolveOutput(OutputRequest{
		Info: info,
		Tpl:  store.Template{Container: "mp4"}.Effective(global),
	})
	if err != nil {
		t.Fatal(err)
	}
	if samePath(out, src) {
		t.Fatalf("refusing to write over the source file: %s", out)
	}
}

// ---------------------------------------------------------------------------
// End-to-end against the real ffmpeg binary.
// ---------------------------------------------------------------------------

func ffmpegBinaries(t *testing.T) media.Binaries {
	t.Helper()
	b := media.Resolve("", "")
	if !b.Ready() {
		t.Skip("ffmpeg / ffprobe not available in PATH")
	}
	return b
}

func makeClip(t *testing.T, bins media.Binaries, path string, w, h int, seconds int, bitrate string) {
	t.Helper()
	args := []string{
		"-hide_banner", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=" + itoa(w) + "x" + itoa(h) + ":rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=" + itoa(seconds),
		"-t", itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-b:v", bitrate, "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k",
		"-shortest",
		path,
	}
	cmd := exec.Command(bins.FFmpeg, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed to build fixture: %v\n%s", err, out)
	}
}

func TestPipelineEndToEnd(t *testing.T) {
	bins := ffmpegBinaries(t)

	work := t.TempDir()
	store.SetDataDir(filepath.Join(work, "data"))

	landscape := filepath.Join(work, "landscape_4k.mp4")
	portrait := filepath.Join(work, "portrait_4k.mp4")
	tiny := filepath.Join(work, "tiny.mp4")

	makeClip(t, bins, landscape, 1280, 720, 4, "4000k")
	makeClip(t, bins, portrait, 720, 1280, 4, "4000k")
	makeClip(t, bins, tiny, 320, 180, 1, "120k")

	// Pretend the fixtures are UHD by overriding what the planner sees is not
	// possible end-to-end, so instead we verify the real command produced the
	// expected geometry for the given sources.
	settings := store.DefaultSettings()
	settings.PreventSleep = false

	// Output rules, concurrency and log level all live on the global template now.
	global := store.DefaultGlobalTemplate()
	global.OutMode = store.OutputSame
	global.OutPattern = "{name}_out"
	global.Perf.Concurrency = 2
	global.Perf.LogLevel = "warning"
	global.Filter = &store.FilterSpec{Action: store.ActionKeep}

	tpl := store.Template{
		ID: "t1", Name: "长边 1280", Container: "mp4",
		VideoMode: store.ModeEncode, VideoCodec: "libx264", RateControl: store.RateCRF, CRF: 28,
		Preset: "ultrafast", PixFmt: "yuv420p",
		Resize:    store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 1280, OnlyLarger: true, MultipleOf: 2, Algorithm: store.ScaleLanczos},
		AudioMode: store.ModeEncode, AudioCodec: "aac", AudioBitrate: "64k",
	}
	tpl.Normalize()

	r := NewRunner()
	r.Configure(Providers{
		Settings:       func() store.Settings { return settings },
		Binaries:       func() media.Binaries { return bins },
		Template:       func(id string) (store.Template, bool) { return tpl, id == "t1" },
		GlobalTemplate: func() store.Template { return global },
	}, nil, nil)

	n, errs := r.AddInputs([]InputItem{
		{Path: landscape}, {Path: portrait}, {Path: tiny},
	}, "t1", tpl.Name)
	if n != 3 {
		t.Fatalf("added %d jobs, want 3 (errors: %v)", n, errs)
	}

	r.Start()
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		st := r.Stats()
		if st.Pending == 0 && st.Running == 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	jobs := r.Jobs()
	byName := map[string]Job{}
	for _, j := range jobs {
		byName[j.InputName] = j
	}

	for _, name := range []string{"landscape_4k.mp4", "portrait_4k.mp4", "tiny.mp4"} {
		j, ok := byName[name]
		if !ok {
			t.Fatalf("missing job for %s", name)
		}
		if j.Status != StatusDone && j.Status != StatusWarning {
			t.Fatalf("%s ended with status %s (error: %s)", name, j.Status, j.Error)
		}
		if j.InfoAfter == nil {
			t.Fatalf("%s produced no output metadata", name)
		}
		if _, err := os.Stat(j.Output); err != nil {
			t.Fatalf("%s output missing: %v", name, err)
		}
	}

	// Landscape 1280x720 with a 1280 long edge -> untouched dimensions.
	if got := byName["landscape_4k.mp4"].InfoAfter; got.DisplayWidth != 1280 || got.DisplayHeight != 720 {
		t.Errorf("landscape output = %dx%d, want 1280x720", got.DisplayWidth, got.DisplayHeight)
	}
	// Portrait 720x1280 is already at the cap.
	if got := byName["portrait_4k.mp4"].InfoAfter; got.DisplayWidth != 720 || got.DisplayHeight != 1280 {
		t.Errorf("portrait output = %dx%d, want 720x1280", got.DisplayWidth, got.DisplayHeight)
	}
	// The tiny clip has a smaller long edge, so onlyLarger must leave it alone.
	if got := byName["tiny.mp4"].InfoAfter; got.DisplayWidth != 320 || got.DisplayHeight != 180 {
		t.Errorf("tiny output = %dx%d, want 320x180 (onlyLarger should skip)", got.DisplayWidth, got.DisplayHeight)
	}

	// Progress and command must have been recorded.
	for _, j := range jobs {
		if j.Command == "" {
			t.Errorf("%s has no recorded command", j.InputName)
		}
		if j.Progress < 0.99 {
			t.Errorf("%s progress = %.2f, want 1.0", j.InputName, j.Progress)
		}
	}
}

func TestFilterRulesMoveExcludedFile(t *testing.T) {
	bins := ffmpegBinaries(t)

	work := t.TempDir()
	store.SetDataDir(filepath.Join(work, "data"))
	target := filepath.Join(work, "small")
	_ = os.MkdirAll(target, 0o755)

	small := filepath.Join(work, "small_clip.mp4")
	makeClip(t, bins, small, 320, 180, 1, "100k")

	settings := store.DefaultSettings()
	settings.PreventSleep = false

	// The filter section -- rules plus where rejected files go -- is now part of
	// the template hierarchy, so it lives on the global template here.
	global := store.DefaultGlobalTemplate()
	global.Filter = &store.FilterSpec{
		MinSizeMB:     500, // the fixture is nowhere near 500 MB
		Action:        store.ActionMove,
		Dest:          store.DestRule{Mode: store.OutputCustom, Dir: target},
		RenamePattern: "excluded_{name}.{ext}",
	}

	tpl := store.Template{ID: "t1", Name: "copy", Container: "", VideoMode: store.ModeCopy, AudioMode: store.ModeCopy}
	tpl.Normalize()

	r := NewRunner()
	r.Configure(Providers{
		Settings:       func() store.Settings { return settings },
		Binaries:       func() media.Binaries { return bins },
		Template:       func(id string) (store.Template, bool) { return tpl, true },
		GlobalTemplate: func() store.Template { return global },
	}, nil, nil)

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
		t.Fatalf("status = %s, want filtered (message: %s)", jobs[0].Status, jobs[0].Message)
	}
	if _, err := os.Stat(small); err == nil {
		t.Error("the excluded file should have been moved away from the source directory")
	}
	moved, err := filepath.Glob(filepath.Join(target, "excluded_small_clip.mp4"))
	if err != nil || len(moved) == 0 {
		t.Fatalf("expected the excluded file in the target directory, got %v (%v)", moved, err)
	}
}

var _ = context.Background
