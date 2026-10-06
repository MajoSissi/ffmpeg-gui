package engine

import (
	"slices"
	"strings"
	"testing"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// A template whose fields are left at their zero values must not have those
// fields forced into the command. ffmpeg has native defaults for all of them, so
// emitting them adds noise and hides what the user actually configured.
//
// The counterpart contract is tested too: an explicitly configured value must
// still reach the command line.

func baseInfo() *media.Info {
	// Video/Audio are set because HasVideo() keys off the stream, not off the
	// dimensions: without them a real ffprobe result and this fixture would
	// disagree about whether there is anything to scale.
	return &media.Info{
		Path: "D:/in/a.mp4", FileName: "a.mp4", Ext: "mp4",
		Duration: 60, Size: 1 << 20,
		Width: 3840, Height: 2160, DisplayWidth: 3840, DisplayHeight: 2160,
		FPS: 30, PixFmt: "yuv420p",
		VideoCodec: "h264", AudioCodec: "aac",
		VideoN: 1, AudioN: 1,
		Video: &media.Stream{Index: 0, Type: "video", Codec: "h264", Width: 3840, Height: 2160, FPS: 30, PixFmt: "yuv420p"},
		Audio: &media.Stream{Index: 1, Type: "audio", Codec: "aac", SampleRate: 48000, Channels: 2},
	}
}

func planArgs(t *testing.T, tpl store.Template) []string {
	t.Helper()
	tpl.Normalize()
	plan, err := BuildPlan(PlanInput{
		Info:   baseInfo(),
		Tpl:    tpl,
		Output: "D:/out/a.mp4",
		Binaries: media.Binaries{
			FFmpeg: "ffmpeg",
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan.Args
}

// hasFlag reports whether flag appears in args, and returns its value if the
// flag takes one.
func hasFlag(args []string, flag string) (bool, string) {
	i := slices.Index(args, flag)
	if i < 0 {
		return false, ""
	}
	if i+1 < len(args) {
		return true, args[i+1]
	}
	return true, ""
}

func TestUnsetVideoCodecIsOmitted(t *testing.T) {
	tpl := store.Template{VideoMode: store.ModeEncode, AudioMode: store.ModeDisable}
	args := planArgs(t, tpl)

	if ok, v := hasFlag(args, "-c:v"); ok {
		t.Errorf("-c:v should be omitted when no encoder is chosen, got %q\nargs: %v", v, args)
	}
	// -crf must go too: without knowing the encoder family we cannot tell whether
	// the correct flag is -crf, -cq or -global_quality.
	if ok, v := hasFlag(args, "-crf"); ok {
		t.Errorf("-crf should be omitted when no encoder is chosen, got %q", v)
	}
}

func TestUnsetVideoCodecWarnsOnlyWhenOptionsAreDropped(t *testing.T) {
	bare := store.Template{VideoMode: store.ModeEncode, AudioMode: store.ModeDisable}
	plan, err := BuildPlan(PlanInput{Info: baseInfo(), Tpl: bare, Output: "o.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("a bare template should stay quiet, got warnings: %v", plan.Warnings)
	}

	configured := store.Template{
		VideoMode: store.ModeEncode, AudioMode: store.ModeDisable,
		RateControl: store.RateCRF, CRF: 24, Preset: "slow",
	}
	plan, err = BuildPlan(PlanInput{Info: baseInfo(), Tpl: configured, Output: "o.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(plan.Warnings, func(w string) bool { return strings.Contains(w, "未指定视频编码器") }) {
		t.Errorf("dropping configured CRF/preset should warn, got: %v", plan.Warnings)
	}
}

func TestUnsetCRFIsOmitted(t *testing.T) {
	tpl := store.Template{
		VideoMode: store.ModeEncode, VideoCodec: "libx264", AudioMode: store.ModeDisable,
		RateControl: store.RateCRF, CRF: 0,
	}
	args := planArgs(t, tpl)

	if ok, v := hasFlag(args, "-crf"); ok {
		t.Errorf("-crf should be omitted when CRF is unset, got %q\nargs: %v", v, args)
	}
	if ok, v := hasFlag(args, "-c:v"); !ok || v != "libx264" {
		t.Errorf("explicit -c:v libx264 should survive, got ok=%v v=%q", ok, v)
	}
}

func TestExplicitCRFIsKept(t *testing.T) {
	tpl := store.Template{
		VideoMode: store.ModeEncode, VideoCodec: "libx264", AudioMode: store.ModeDisable,
		RateControl: store.RateCRF, CRF: 24,
	}
	args := planArgs(t, tpl)

	if ok, v := hasFlag(args, "-crf"); !ok || v != "24" {
		t.Errorf("explicit CRF should be kept, got ok=%v v=%q\nargs: %v", ok, v, args)
	}
}

func TestAV1KeepsBitrateZeroAlongsideCRF(t *testing.T) {
	tpl := store.Template{
		VideoMode: store.ModeEncode, VideoCodec: "libsvtav1", AudioMode: store.ModeDisable,
		RateControl: store.RateCRF, CRF: 30,
	}
	args := planArgs(t, tpl)

	// -b:v 0 is not decorative for SVT-AV1: without it CRF becomes constrained
	// quality. It must appear together with -crf, and must not appear without it.
	if ok, v := hasFlag(args, "-b:v"); !ok || v != "0" {
		t.Errorf("SVT-AV1 with CRF needs -b:v 0, got ok=%v v=%q\nargs: %v", ok, v, args)
	}

	tpl.CRF = 0
	args = planArgs(t, tpl)
	if ok, _ := hasFlag(args, "-b:v"); ok {
		t.Errorf("-b:v 0 is meaningless without -crf, should be omitted\nargs: %v", args)
	}
}

func TestUnsetAudioCodecIsOmitted(t *testing.T) {
	tpl := store.Template{VideoMode: store.ModeCopy, AudioMode: store.ModeEncode}
	args := planArgs(t, tpl)

	if ok, v := hasFlag(args, "-c:a"); ok {
		t.Errorf("-c:a should be omitted when no audio encoder is chosen, got %q\nargs: %v", v, args)
	}
}

func TestAudioOptionsSurviveWithoutCodec(t *testing.T) {
	// -b:a / -ac / -ar are meaningful for whichever encoder ffmpeg picks, so they
	// must not be dropped just because -c:a was.
	tpl := store.Template{
		VideoMode: store.ModeCopy, AudioMode: store.ModeEncode,
		AudioBitrate: "192k", AudioChannels: 2, SampleRate: 48000,
	}
	args := planArgs(t, tpl)

	for _, want := range [][2]string{{"-b:a", "192k"}, {"-ac", "2"}, {"-ar", "48000"}} {
		if ok, v := hasFlag(args, want[0]); !ok || v != want[1] {
			t.Errorf("expected %s %s, got ok=%v v=%q\nargs: %v", want[0], want[1], ok, v, args)
		}
	}
}

func TestExplicitAudioCodecIsKept(t *testing.T) {
	tpl := store.Template{
		VideoMode: store.ModeCopy, AudioMode: store.ModeEncode, AudioCodec: "libopus",
	}
	args := planArgs(t, tpl)

	if ok, v := hasFlag(args, "-c:a"); !ok || v != "libopus" {
		t.Errorf("explicit audio codec should be kept, got ok=%v v=%q", ok, v)
	}
}

func TestMaxMuxQueueIsOptIn(t *testing.T) {
	tpl := store.Template{VideoMode: store.ModeCopy, AudioMode: store.ModeCopy}
	args := planArgs(t, tpl)
	if ok, _ := hasFlag(args, "-max_muxing_queue_size"); ok {
		t.Errorf("-max_muxing_queue_size should be omitted by default\nargs: %v", args)
	}

	tpl.MaxMuxQueue = 2048
	args = planArgs(t, tpl)
	if ok, v := hasFlag(args, "-max_muxing_queue_size"); !ok || v != "2048" {
		t.Errorf("explicit MaxMuxQueue should be emitted, got ok=%v v=%q", ok, v)
	}
}

func TestMuxQueueStaysAfterInput(t *testing.T) {
	// Output options that appear before the first -i make ffmpeg fail with
	// "Option ... cannot be applied to input url".
	tpl := store.Template{
		VideoMode: store.ModeEncode, VideoCodec: "libx264", CRF: 24,
		AudioMode: store.ModeEncode, AudioCodec: "aac", MaxMuxQueue: 2048,
	}
	args := planArgs(t, tpl)

	iInput := slices.Index(args, "-i")
	iQueue := slices.Index(args, "-max_muxing_queue_size")
	if iInput < 0 || iQueue < 0 {
		t.Fatalf("expected both -i and -max_muxing_queue_size, got %v", args)
	}
	if iQueue < iInput {
		t.Errorf("-max_muxing_queue_size must come after -i\nargs: %v", args)
	}
	if last := args[len(args)-1]; last != "D:/out/a.mp4" {
		t.Errorf("output path must be last, got %q", last)
	}
}

func TestRequiredFlagsAlwaysPresent(t *testing.T) {
	args := planArgs(t, store.Template{VideoMode: store.ModeCopy, AudioMode: store.ModeCopy})

	// -progress drives the live speed/bitrate/percent readout, -nostdin and -y
	// keep batch runs from stalling on a prompt, -hide_banner trims the log.
	for _, flag := range []string{"-hide_banner", "-nostdin", "-y", "-progress"} {
		if ok, _ := hasFlag(args, flag); !ok {
			t.Errorf("required flag %s is missing from %v", flag, args)
		}
	}
	if ok, v := hasFlag(args, "-progress"); !ok || v != "pipe:1" {
		t.Errorf("-progress must point at pipe:1, got %q", v)
	}
}

func TestThreadsOmittedWhenUnset(t *testing.T) {
	args := planArgs(t, store.Template{VideoMode: store.ModeCopy, AudioMode: store.ModeCopy})
	if ok, v := hasFlag(args, "-threads"); ok {
		t.Errorf("-threads must be omitted when unconfigured, got %q\nargs: %v", v, args)
	}
}

func TestThreadsEmittedWhenSet(t *testing.T) {
	tpl := store.Template{VideoMode: store.ModeCopy, AudioMode: store.ModeCopy}
	in := PlanInput{
		Info:     baseInfo(),
		Tpl:      tpl,
		Output:   "D:/out/a.mp4",
		Binaries: media.Binaries{FFmpeg: "ffmpeg"},
		Threads:  8,
	}
	tpl.Normalize()
	plan, err := BuildPlan(in)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	ok, v := hasFlag(plan.Args, "-threads")
	if !ok || v != "8" {
		t.Fatalf("-threads 8 expected, got %q\nargs: %v", v, plan.Args)
	}
	// It belongs to the output, so it has to sit after the input.
	if i := slices.Index(plan.Args, "-i"); i < 0 || slices.Index(plan.Args, "-threads") < i {
		t.Errorf("-threads must come after -i\nargs: %v", plan.Args)
	}
}

// ---------------------------------------------------------------------------
// Scaling: only the axis the template pins is written out
// ---------------------------------------------------------------------------

func portraitInfo() *media.Info {
	info := baseInfo()
	info.Width, info.Height = 2160, 3840
	info.DisplayWidth, info.DisplayHeight = 2160, 3840
	return info
}

// scaleFilter returns the -vf value a resize spec produces, failing when the
// plan carries no filter at all.
func scaleFilter(t *testing.T, info *media.Info, r store.ResizeSpec) string {
	t.Helper()
	tpl := store.Template{
		VideoMode: store.ModeEncode, VideoCodec: "libx264", AudioMode: store.ModeCopy, Resize: r,
	}
	tpl.Normalize()
	plan, err := BuildPlan(PlanInput{
		Info:     info,
		Tpl:      tpl,
		Output:   "D:/out/a.mp4",
		Binaries: media.Binaries{FFmpeg: "ffmpeg"},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	ok, v := hasFlag(plan.Args, "-vf")
	if !ok {
		t.Fatalf("no -vf in args: %v", plan.Args)
	}
	return v
}

func TestLongEdgePinsOnlyOneAxis(t *testing.T) {
	// 3840x2160 with a 2560 long edge: the width is the long edge, so only it is
	// written out. ffmpeg works the height out, and -2 keeps it even.
	r := store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, MultipleOf: 2, Algorithm: store.ScaleLanczos}
	if got := scaleFilter(t, baseInfo(), r); got != "scale=2560:-2:flags=lanczos" {
		t.Errorf("landscape long edge = %q, want scale=2560:-2:flags=lanczos", got)
	}
	// The same template on a portrait clip pins the height instead.
	if got := scaleFilter(t, portraitInfo(), r); got != "scale=-2:2560:flags=lanczos" {
		t.Errorf("portrait long edge = %q, want scale=-2:2560:flags=lanczos", got)
	}
}

func TestShortEdgePinsOnlyOneAxis(t *testing.T) {
	r := store.ResizeSpec{Mode: store.ResizeShortEdge, ShortEdge: 1080, MultipleOf: 2, Algorithm: store.ScaleLanczos}
	if got := scaleFilter(t, baseInfo(), r); got != "scale=-2:1080:flags=lanczos" {
		t.Errorf("landscape short edge = %q, want scale=-2:1080:flags=lanczos", got)
	}
	if got := scaleFilter(t, portraitInfo(), r); got != "scale=1080:-2:flags=lanczos" {
		t.Errorf("portrait short edge = %q, want scale=1080:-2:flags=lanczos", got)
	}
}

// The auto side is -1 with no alignment and -n with one, which is what makes the
// "对齐倍数" box keep working now that ffmpeg does the arithmetic.
func TestAutoDimFollowsMultipleOf(t *testing.T) {
	r := store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, MultipleOf: 1, Algorithm: store.ScaleLanczos}
	if got := scaleFilter(t, baseInfo(), r); got != "scale=2560:-1:flags=lanczos" {
		t.Errorf("multipleOf 1 = %q, want scale=2560:-1:flags=lanczos", got)
	}
	r.MultipleOf = 8
	if got := scaleFilter(t, baseInfo(), r); got != "scale=2560:-8:flags=lanczos" {
		t.Errorf("multipleOf 8 = %q, want scale=2560:-8:flags=lanczos", got)
	}
}

// An unset algorithm must not smuggle a :flags= into the command: writing
// flags=bicubic would claim a choice the user never made.
func TestScaleAlgorithmOmittedWhenUnset(t *testing.T) {
	r := store.ResizeSpec{Mode: store.ResizeLongEdge, LongEdge: 2560, MultipleOf: 2}
	if got := scaleFilter(t, baseInfo(), r); got != "scale=2560:-2" {
		t.Errorf("unset algorithm = %q, want scale=2560:-2 with no :flags=", got)
	}
}

// An exact size with one axis left at 0 keeps the auto side too; pad needs both,
// so it is the one case that still writes two numbers.
func TestExactResizeKeepsBothAxes(t *testing.T) {
	r := store.ResizeSpec{Mode: store.ResizeExact, Width: 1280, MultipleOf: 2, Algorithm: store.ScaleLanczos}
	if got := scaleFilter(t, baseInfo(), r); got != "scale=1280:-2:flags=lanczos" {
		t.Errorf("exact width only = %q, want scale=1280:-2:flags=lanczos", got)
	}
	r.Height = 720
	if got := scaleFilter(t, baseInfo(), r); got != "scale=1280:720:flags=lanczos" {
		t.Errorf("exact both = %q, want scale=1280:720:flags=lanczos", got)
	}
}
