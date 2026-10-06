package engine

import (
	"fmt"
	"math"
	"strings"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// PlanInput is everything needed to turn a template into a command line.
type PlanInput struct {
	Info     *media.Info
	Tpl      store.Template
	Settings store.Settings
	Binaries media.Binaries
	Output   string
	// LogLevel is the template's -loglevel, and Threads the -threads cap. They
	// normally come from the merged template's Perf section; the fields exist so
	// the caller can pass them without having to reach into Tpl.Perf (which is
	// nil when the template follows the global one).
	LogLevel string
	Threads  int
}

// Plan is the executable outcome.
type Plan struct {
	// Bin is the ffmpeg executable the args belong to, and Args is the flat
	// argument list. The UI renders one argument per line from these rather than
	// showing Command, which is a single unreadable wrapped line.
	Bin       string   `json:"bin"`
	Args      []string `json:"args"`
	Command   string   `json:"command"`
	Warnings  []string `json:"warnings"`
	TargetW   int      `json:"targetW"`
	TargetH   int      `json:"targetH"`
	Resized   bool     `json:"resized"`
	OutputExt string   `json:"outputExt"`
}

// BuildPlan assembles the ffmpeg argument list for one input file.
func BuildPlan(in PlanInput) (*Plan, error) {
	t := in.Tpl
	info := in.Info
	if info == nil {
		return nil, fmt.Errorf("缺少媒体信息")
	}
	plan := &Plan{}

	outExt := ""
	if c := strings.TrimSpace(t.Container); c != "" {
		outExt = ContainerExt(c)
	} else if info.Ext != "" {
		outExt = ContainerExt(info.Ext)
	}
	plan.OutputExt = outExt

	// -------- stream mapping --------
	// Global options only may appear before the first -i; everything that
	// belongs to the muxer is appended after the input below.
	args := []string{"-hide_banner", "-nostdin", "-y"}
	if lv := strings.ToLower(in.LogLevel); lv != "" && lv != "warning" {
		args = append(args, "-loglevel", lv)
	}
	args = append(args, "-progress", "pipe:1")

	if in.Settings.HardwareDecode {
		args = append(args, "-hwaccel", "auto")
	}
	args = append(args, SplitArgs(in.Settings.GlobalInArgs)...)
	for _, a := range t.InputArgs {
		if a.Enabled && strings.TrimSpace(a.Flag) != "" {
			args = append(args, strings.TrimSpace(a.Flag))
			if strings.TrimSpace(a.Value) != "" {
				args = append(args, SplitArgs(a.Value)...)
			}
		}
	}

	args = append(args, "-i", info.Path)

	args, mapWarn := appendMapping(args, t, outExt, info)
	plan.Warnings = append(plan.Warnings, mapWarn...)

	// -------- video --------
	var vWarn []string
	args, vWarn = appendVideoArgs(args, t, info)
	plan.Warnings = append(plan.Warnings, vWarn...)

	// -------- filters --------
	filterArgs, fWarn, tw, th, resized := appendFilterArgs(t, info)
	args = append(args, filterArgs...)
	plan.Warnings = append(plan.Warnings, fWarn...)
	plan.TargetW, plan.TargetH, plan.Resized = tw, th, resized

	// -------- audio --------
	args = append(args, audioArgs(t)...)

	// -------- container / metadata --------
	// Emitted only when the template asks for it. ffmpeg has a working default and
	// most sources never need this; it exists for inputs that burst packets
	// (damaged or variable-frame-rate), which would otherwise abort the muxer with
	// "Too many packets buffered for output stream".
	if t.MaxMuxQueue > 0 {
		args = append(args, "-max_muxing_queue_size", itoa(t.MaxMuxQueue))
	}
	// How many CPU cores one ffmpeg may spread its work over. Omitted when unset:
	// ffmpeg already picks a workable number per codec, and this only earns its
	// place when you want to leave cores free while a batch runs.
	if in.Threads > 0 {
		args = append(args, "-threads", itoa(in.Threads))
	}
	if t.FastStart && (outExt == "mp4" || outExt == "mov" || outExt == "m4a" || outExt == "m4v") {
		args = append(args, "-movflags", "+faststart")
	}
	if t.StripMetadata {
		args = append(args, "-map_metadata", "-1")
	}
	if t.StripChapters {
		args = append(args, "-map_chapters", "-1")
	}

	// -------- custom output args --------
	for _, a := range t.OutputArgs {
		if a.Enabled && strings.TrimSpace(a.Flag) != "" {
			args = append(args, strings.TrimSpace(a.Flag))
			if strings.TrimSpace(a.Value) != "" {
				args = append(args, SplitArgs(a.Value)...)
			}
		}
	}
	args = append(args, SplitArgs(in.Settings.GlobalOutArgs)...)

	plan.Warnings = append(plan.Warnings, compatibilityWarnings(t, info, outExt)...)

	args = append(args, in.Output)
	plan.Bin = in.Binaries.FFmpeg
	plan.Args = args
	plan.Command = JoinArgs(plan.Bin, args)
	if plan.Command == " " {
		plan.Bin = "ffmpeg"
		plan.Command = "ffmpeg"
	}
	return plan, nil
}

// ---------------------------------------------------------------------------
// Stream mapping
// ---------------------------------------------------------------------------

func appendMapping(args []string, t store.Template, outExt string, info *media.Info) ([]string, []string) {
	var warn []string

	if !t.MapAll {
		// Default stream selection: one video + one audio, no subtitles.
		//
		// -sn is what keeps subtitles out. It is not a hidden constant -- it is the
		// other side of the editor's 「保留全部流」 switch: turning that on replaces
		// this with -map 0 plus a container-appropriate -c:s below.
		args = append(args, "-sn")
		return args, warn
	}

	args = append(args, "-map", "0")
	if t.VideoMode == store.ModeDisable {
		args = append(args, "-map", "-0:v?")
	}
	if t.AudioMode == store.ModeDisable {
		args = append(args, "-map", "-0:a?")
	}

	switch outExt {
	case "mp4", "mov", "m4v":
		args = append(args, "-c:s", "mov_text")
		// mp4 cannot carry attachments or arbitrary data streams.
		args = append(args, "-map", "-0:t?", "-map", "-0:d?")
	case "mkv", "mka":
		args = append(args, "-c:s", "copy")
	case "webm":
		args = append(args, "-c:s", "webvtt")
		args = append(args, "-map", "-0:t?", "-map", "-0:d?")
	default:
		args = append(args, "-map", "-0:s?", "-map", "-0:t?", "-map", "-0:d?")
	}

	if info.SubtitleN > 0 && outExt == "mp4" && t.VideoMode == store.ModeCopy {
		warn = append(warn, "复制视频流到 mp4 时字幕需转为 mov_text，若源字幕为图形字幕（PGS/VobSub）可能失败")
	}
	return args, warn
}

// ---------------------------------------------------------------------------
// Video codec
// ---------------------------------------------------------------------------

func appendVideoArgs(args []string, t store.Template, info *media.Info) ([]string, []string) {
	var warn []string

	switch t.VideoMode {
	case store.ModeDisable:
		return append(args, "-vn"), warn
	case store.ModeCopy:
		if t.VideoCodec != "" && !strings.EqualFold(t.VideoCodec, "copy") {
			warn = append(warn, "视频模式为「直接复制」时，指定的编码器会被忽略")
		}
		return append(args, "-c:v", "copy"), warn
	}

	codec := strings.TrimSpace(t.VideoCodec)
	if codec == "" {
		// No encoder picked: leave -c:v out entirely so ffmpeg uses its own default
		// for the container. Rate-control flags cannot be emitted either, because
		// the right flag depends on the encoder family (-crf for lib*, -cq for
		// NVENC, -global_quality for QSV ...) and guessing would be worse than
		// omitting. Warn only when the user actually configured something that is
		// now being skipped, so a deliberately bare template stays quiet.
		if t.CRF > 0 || strings.TrimSpace(t.VideoBitrate) != "" || strings.TrimSpace(t.Preset) != "" {
			warn = append(warn, "未指定视频编码器：将使用 ffmpeg 的默认编码器，已跳过 CRF、码率与 preset 等编码器相关参数")
		}
		return args, warn
	}
	kind := codecKind(codec)
	args = append(args, "-c:v", codec)

	rc := t.RateControl
	if rc == "" {
		rc = store.RateCRF
	}
	crf := t.CRF

	switch kind {
	case "lib":
		switch rc {
		case store.RateBitrate:
			if t.VideoBitrate != "" {
				args = append(args, "-b:v", t.VideoBitrate)
			}
		case store.RateQP:
			if t.CRF > 0 {
				args = append(args, "-qp", itoa(crf))
			}
		default:
			// CRF of 0 means "not set". libx264/libx265 already default to 23, so
			// emitting nothing gives the same encoder behaviour while keeping the
			// command an honest record of what was actually configured.
			if t.CRF > 0 {
				args = append(args, "-crf", itoa(crf))
				if strings.Contains(codec, "vpx") || strings.Contains(codec, "aom") || strings.Contains(codec, "svtav1") {
					args = append(args, "-b:v", "0")
				}
			}
		}
	case "nvenc":
		switch rc {
		case store.RateBitrate:
			if t.VideoBitrate != "" {
				args = append(args, "-b:v", t.VideoBitrate)
			}
		case store.RateQP:
			if t.CRF > 0 {
				args = append(args, "-rc", "constqp", "-qp", itoa(crf))
			}
		default:
			if t.CRF > 0 {
				args = append(args, "-rc", "vbr", "-cq", itoa(crf), "-b:v", "0")
			} else if t.VideoBitrate != "" {
				// -cq needs a quality value; without one, fall back to the bitrate
				// the user did specify rather than inventing a CRF.
				args = append(args, "-b:v", t.VideoBitrate)
			}
		}
	case "qsv":
		if rc == store.RateBitrate && t.VideoBitrate != "" {
			args = append(args, "-b:v", t.VideoBitrate)
		} else if t.CRF > 0 {
			args = append(args, "-global_quality", itoa(crf))
		}
	case "amf":
		if rc == store.RateBitrate && t.VideoBitrate != "" {
			args = append(args, "-b:v", t.VideoBitrate)
		} else if t.CRF > 0 {
			args = append(args, "-rc", "cqp", "-qp_i", itoa(crf), "-qp_p", itoa(crf))
		}
	case "vt":
		if t.CRF > 0 {
			q := 100 - crf
			if q < 1 {
				q = 1
			}
			if q > 100 {
				q = 100
			}
			args = append(args, "-q:v", itoa(q))
			warn = append(warn, "VideoToolbox 使用 -q:v 质量档位，CRF 值已近似换算")
		}
	case "image":
		if t.CRF > 0 {
			warn = append(warn, codec+" 编码器不使用 CRF，已忽略相关参数")
		}
	default:
		if rc == store.RateCRF && t.CRF > 0 {
			warn = append(warn, "未知编码器 "+codec+"，已按 -crf 传递，若不被支持请在模板中用自定义参数覆盖")
			args = append(args, "-crf", itoa(crf))
		}
	}

	// preset
	if p := strings.TrimSpace(t.Preset); p != "" {
		switch kind {
		case "lib", "qsv":
			args = append(args, "-preset", p)
		case "nvenc":
			args = append(args, "-preset", nvencPreset(p))
		case "amf":
			warn = append(warn, "AMF 不支持 -preset，已忽略")
		case "vt":
			warn = append(warn, "VideoToolbox 不使用 -preset，请在自定义参数中设置 -allow_sw / -realtime")
		}
	}
	if tn := strings.TrimSpace(t.Tune); tn != "" {
		if kind == "lib" {
			args = append(args, "-tune", tn)
		} else {
			warn = append(warn, "-tune 仅对 libx264/libx265 生效，已忽略")
		}
	}
	if pr := strings.TrimSpace(t.Profile); pr != "" {
		switch kind {
		case "lib", "nvenc", "qsv":
			args = append(args, "-profile:v", pr)
		}
	}
	if lv := strings.TrimSpace(t.Level); lv != "" {
		if kind == "lib" || kind == "nvenc" {
			args = append(args, "-level", lv)
		}
	}
	if pf := strings.TrimSpace(t.PixFmt); pf != "" {
		args = append(args, "-pix_fmt", pf)
	}
	if strings.TrimSpace(t.MaxRate) != "" {
		args = append(args, "-maxrate", strings.TrimSpace(t.MaxRate))
	}
	if strings.TrimSpace(t.BufSize) != "" {
		args = append(args, "-bufsize", strings.TrimSpace(t.BufSize))
	}
	return args, warn
}

func codecKind(codec string) string {
	c := strings.ToLower(codec)
	switch {
	case c == "copy" || c == "":
		return "copy"
	case strings.HasPrefix(c, "libx26"), strings.HasPrefix(c, "libxvid"),
		strings.HasPrefix(c, "libvpx"), strings.HasPrefix(c, "libaom"),
		strings.HasPrefix(c, "libsvtav1"), strings.HasPrefix(c, "librav1e"),
		strings.HasPrefix(c, "libopenh264"):
		return "lib"
	case strings.HasSuffix(c, "_nvenc"):
		return "nvenc"
	case strings.HasSuffix(c, "_qsv"):
		return "qsv"
	case strings.HasSuffix(c, "_amf"):
		return "amf"
	case strings.HasSuffix(c, "_videotoolbox"):
		return "vt"
	case c == "gif" || c == "webp" || c == "apng" || c == "png" || c == "mjpeg":
		return "image"
	default:
		return "other"
	}
}

// nvencPreset maps x264-style preset names onto the NVENC p1..p7 ladder.
func nvencPreset(p string) string {
	low := strings.ToLower(strings.TrimSpace(p))
	if len(low) == 2 && low[0] == 'p' && low[1] >= '1' && low[1] <= '7' {
		return low
	}
	switch low {
	case "ultrafast", "superfast":
		return "p1"
	case "veryfast":
		return "p2"
	case "faster", "fast":
		return "p3"
	case "medium":
		return "p4"
	case "slow":
		return "p5"
	case "slower":
		return "p6"
	case "veryslow":
		return "p7"
	default:
		return "p4"
	}
}

// ---------------------------------------------------------------------------
// Filters (scale / fps / custom)
// ---------------------------------------------------------------------------

func appendFilterArgs(t store.Template, info *media.Info) (args []string, warn []string, tw, th int, resized bool) {
	if t.VideoMode == store.ModeDisable || t.VideoMode == store.ModeCopy {
		if t.Resize.Enabled() && info != nil && info.HasVideo() {
			warn = append(warn, "视频流为「直接复制」，分辨率设置不会生效（需要重新编码）")
		}
		return nil, warn, 0, 0, false
	}

	var chain []string

	if sw, sh, filter, did, w := computeScale(info, t.Resize); did {
		chain = append(chain, filter)
		tw, th, resized = sw, sh, true
		if w != "" {
			warn = append(warn, w)
		}
	}

	if f := strings.TrimSpace(t.FPS); f != "" && f != "0" {
		chain = append(chain, "fps="+f)
	}

	custom := strings.TrimSpace(t.VideoFilters)
	if custom != "" {
		if strings.EqualFold(t.FilterMode, "complex") {
			if len(chain) > 0 {
				warn = append(warn, "已启用复杂滤镜图，自动缩放被跳过；请把 scale 写进 -filter_complex")
			}
			return []string{"-filter_complex", custom}, warn, 0, 0, false
		}
		chain = append(chain, custom)
	}

	if len(chain) == 0 {
		return nil, warn, tw, th, resized
	}
	return []string{"-vf", strings.Join(chain, ",")}, warn, tw, th, resized
}

// computeScale resolves the target geometry. It works in display space so a
// portrait clip tagged with a 90° rotation is handled like any other portrait
// clip: the long edge is the long edge.
//
// Only the axis the user actually pinned is written into the filter. The other one
// goes to ffmpeg as -1 (or -n when an alignment is asked for), which keeps the
// aspect ratio and rounds to that multiple. Computing both sides here and handing
// ffmpeg two finished numbers meant the program was deciding the geometry, and any
// difference between that guess and what ffmpeg really produces stayed invisible.
//
// tw/th are still returned for display (the preview and the history record); for
// the auto modes they are the value ffmpeg will land on, computed the same way.
func computeScale(info *media.Info, r store.ResizeSpec) (tw, th int, filter string, applied bool, warn string) {
	if !r.Enabled() || info == nil || !info.HasVideo() {
		return 0, 0, "", false, ""
	}
	srcW, srcH := info.DisplayWidth, info.DisplayHeight
	if srcW <= 0 || srcH <= 0 {
		return 0, 0, "", false, ""
	}

	mult := r.MultipleOf
	if mult <= 0 {
		mult = 2
	}
	algo := r.Algorithm

	var targetW, targetH int
	// What goes into scale=w:h. Left blank by the modes that compute both sides;
	// the auto modes fill one of them with -1 / -n instead of a number.
	exprW, exprH := "", ""

	switch r.Mode {
	case store.ResizeLongEdge:
		long := maxInt(srcW, srcH)
		if r.LongEdge <= 0 {
			return 0, 0, "", false, "未填写长边像素值"
		}
		if r.OnlyLarger && long <= r.LongEdge {
			return 0, 0, "", false, ""
		}
		ratio := float64(r.LongEdge) / float64(long)
		targetW = alignTo(int(math.Round(float64(srcW)*ratio)), mult)
		targetH = alignTo(int(math.Round(float64(srcH)*ratio)), mult)
		if srcW >= srcH {
			exprW, exprH = itoa(r.LongEdge), autoDim(mult)
		} else {
			exprW, exprH = autoDim(mult), itoa(r.LongEdge)
		}

	case store.ResizeShortEdge:
		short := minInt(srcW, srcH)
		if r.ShortEdge <= 0 {
			return 0, 0, "", false, "未填写短边像素值"
		}
		if r.OnlyLarger && short <= r.ShortEdge {
			return 0, 0, "", false, ""
		}
		ratio := float64(r.ShortEdge) / float64(short)
		targetW = alignTo(int(math.Round(float64(srcW)*ratio)), mult)
		targetH = alignTo(int(math.Round(float64(srcH)*ratio)), mult)
		if srcW <= srcH {
			exprW, exprH = itoa(r.ShortEdge), autoDim(mult)
		} else {
			exprW, exprH = autoDim(mult), itoa(r.ShortEdge)
		}

	case store.ResizeExact:
		switch {
		case r.Width > 0 && r.Height > 0:
			targetW, targetH = r.Width, r.Height
		case r.Width > 0:
			targetW = r.Width
			targetH = alignTo(int(math.Round(float64(srcH)*float64(r.Width)/float64(srcW))), mult)
			exprW, exprH = itoa(r.Width), autoDim(mult)
		case r.Height > 0:
			targetH = r.Height
			targetW = alignTo(int(math.Round(float64(srcW)*float64(r.Height)/float64(srcH))), mult)
			exprW, exprH = autoDim(mult), itoa(r.Height)
		default:
			return 0, 0, "", false, "未填写目标宽高"
		}

	case store.ResizeFit:
		maxW, maxH := r.MaxWidth, r.MaxHeight
		if maxW <= 0 {
			maxW = srcW
		}
		if maxH <= 0 {
			maxH = srcH
		}
		scale := math.Min(float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
		if scale >= 1 {
			return 0, 0, "", false, ""
		}
		targetW = alignTo(int(math.Round(float64(srcW)*scale)), mult)
		targetH = alignTo(int(math.Round(float64(srcH)*scale)), mult)

	case store.ResizePercent:
		if r.Percent <= 0 {
			return 0, 0, "", false, "缩放百分比无效"
		}
		if r.OnlyLarger && r.Percent >= 100 {
			return 0, 0, "", false, ""
		}
		ratio := r.Percent / 100
		targetW = alignTo(int(math.Round(float64(srcW)*ratio)), mult)
		targetH = alignTo(int(math.Round(float64(srcH)*ratio)), mult)

	default:
		return 0, 0, "", false, ""
	}

	targetW = evenKeep(targetW)
	targetH = evenKeep(targetH)
	if targetW == srcW && targetH == srcH {
		return 0, 0, "", false, ""
	}
	if exprW == "" {
		exprW, exprH = itoa(targetW), itoa(targetH)
	}

	if r.PadToTarget && r.Mode == store.ResizeExact && r.Width > 0 && r.Height > 0 {
		color := r.PadColor
		if color == "" {
			color = "black"
		}
		// Scale down to fit, then letterbox to the exact target. Both numbers have
		// to be real here: pad needs a box to fill, so there is no auto side.
		scale := math.Min(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
		fitW := evenKeep(alignTo(int(math.Round(float64(srcW)*scale)), mult))
		fitH := evenKeep(alignTo(int(math.Round(float64(srcH)*scale)), mult))
		filter = fmt.Sprintf(
			"%s,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:%s",
			scaleExpr(itoa(fitW), itoa(fitH), algo), targetW, targetH, color)
		return targetW, targetH, filter, true, ""
	}

	filter = scaleExpr(exprW, exprH, algo)
	if !r.OnlyLarger && (targetW > srcW || targetH > srcH) {
		warn = "目标分辨率大于源视频，画面会被放大"
	}
	return targetW, targetH, filter, true, warn
}

// autoDim is the value handed to ffmpeg for the axis the template did not pin:
// -1 keeps the aspect ratio, -n additionally rounds that side to a multiple of n.
// The default alignment of 2 therefore comes out as -2, which is what most
// encoders need anyway.
func autoDim(mult int) string {
	if mult > 1 {
		return "-" + itoa(mult)
	}
	return "-1"
}

// scaleExpr renders one scale filter. An empty algorithm leaves :flags= out, which
// is the only honest way to say "whatever ffmpeg defaults to" -- writing
// flags=bicubic here would claim a choice the user never made.
func scaleExpr(w, h, algo string) string {
	if algo == "" {
		return "scale=" + w + ":" + h
	}
	return "scale=" + w + ":" + h + ":flags=" + algo
}

// ---------------------------------------------------------------------------
// Audio
// ---------------------------------------------------------------------------

func audioArgs(t store.Template) []string {
	switch t.AudioMode {
	case store.ModeDisable:
		return []string{"-an"}
	case store.ModeCopy:
		return []string{"-c:a", "copy"}
	default:
		// An empty codec means "let ffmpeg choose", same as the video side: no
		// -c:a at all. The remaining audio options are still honoured, since they
		// are meaningful for whatever encoder ffmpeg picks.
		codec := strings.TrimSpace(t.AudioCodec)
		var args []string
		if codec != "" {
			args = append(args, "-c:a", codec)
		}
		if strings.TrimSpace(t.AudioBitrate) != "" {
			args = append(args, "-b:a", strings.TrimSpace(t.AudioBitrate))
		}
		if t.AudioChannels > 0 {
			args = append(args, "-ac", itoa(t.AudioChannels))
		}
		if t.SampleRate > 0 {
			args = append(args, "-ar", itoa(t.SampleRate))
		}
		return args
	}
}

// ---------------------------------------------------------------------------
// Compatibility hints
// ---------------------------------------------------------------------------

func compatibilityWarnings(t store.Template, info *media.Info, outExt string) []string {
	var warn []string
	c := strings.ToLower(t.VideoCodec)

	containerOK := func(ext, codec string) bool {
		switch ext {
		case "mp4", "m4v", "mov":
			switch codec {
			case "libx264", "libx265", "h264", "hevc", "mpeg4", "libxvid", "av1", "libsvtav1", "libaom-av1", "h264_nvenc", "hevc_nvenc", "h264_qsv", "hevc_qsv", "h264_amf", "hevc_amf", "h264_videotoolbox", "hevc_videotoolbox", "copy", "":
				return true
			}
			return false
		case "webm":
			switch codec {
			case "libvpx", "libvpx-vp9", "vp8", "vp9", "av1", "libsvtav1", "libaom-av1", "copy", "":
				return true
			}
			return false
		}
		return true
	}

	if outExt != "" && !containerOK(outExt, c) {
		warn = append(warn, "编码器 "+c+" 通常不能封装进 ."+outExt+"，建议改用 MKV 或更换编码器")
	}

	if t.VideoMode == store.ModeCopy && t.Resize.Enabled() {
		warn = append(warn, "复制视频流时无法改变分辨率")
	}
	if t.VideoMode == store.ModeCopy && outExt == "mp4" && info != nil {
		if strings.Contains(info.Container, "matroska") {
			warn = append(warn, "MKV → MP4 重封装对部分编码不安全，若失败请改用 MKV 容器")
		}
	}
	if outExt == "webm" && t.AudioMode == store.ModeEncode {
		switch strings.ToLower(t.AudioCodec) {
		case "opus", "libopus", "vorbis", "libvorbis", "":
		default:
			warn = append(warn, "WebM 只支持 Opus / Vorbis 音频")
		}
	}
	if t.FastStart && outExt != "mp4" && outExt != "mov" && outExt != "m4a" && outExt != "m4v" {
		warn = append(warn, "faststart 仅对 MP4/MOV 系列容器生效，已忽略")
	}
	return warn
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
