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
	return &media.Info{
		Path: "D:/in/a.mp4", FileName: "a.mp4", Ext: "mp4",
		Duration: 60, Size: 1 << 20,
		Width: 3840, Height: 2160, DisplayWidth: 3840, DisplayHeight: 2160,
		FPS: 30, PixFmt: "yuv420p",
		VideoCodec: "h264", AudioCodec: "aac",
		VideoN: 1, AudioN: 1,
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
