package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ffmpeggui/internal/sysx"
)

// Stream is a flattened view of one ffprobe stream.
type Stream struct {
	Index         int     `json:"index"`
	Type          string  `json:"type"`
	Codec         string  `json:"codec"`
	CodecLong     string  `json:"codecLong"`
	Profile       string  `json:"profile"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	FPS           float64 `json:"fps"`
	BitRate       int64   `json:"bitRate"`
	PixFmt        string  `json:"pixFmt"`
	SampleRate    int     `json:"sampleRate"`
	Channels      int     `json:"channels"`
	ChannelLayout string  `json:"channelLayout"`
	Language      string  `json:"language"`
	Title         string  `json:"title"`
	Default       bool    `json:"default"`
}

// Info is everything the planner needs to know about an input file.
type Info struct {
	Path          string  `json:"path"`
	FileName      string  `json:"fileName"`
	Ext           string  `json:"ext"`
	Size          int64   `json:"size"`
	Container     string  `json:"container"`
	ContainerLong string  `json:"containerLong"`
	Duration      float64 `json:"duration"`
	BitRate       int64   `json:"bitRate"`
	Rotation      int     `json:"rotation"`

	Width         int `json:"width"` // coded size
	Height        int `json:"height"`
	DisplayWidth  int `json:"displayWidth"` // rotation applied
	DisplayHeight int `json:"displayHeight"`

	FPS        float64 `json:"fps"`
	PixFmt     string  `json:"pixFmt"`
	VideoCodec string  `json:"videoCodec"`
	AudioCodec string  `json:"audioCodec"`

	Video     *Stream `json:"video"`
	Audio     *Stream `json:"audio"`
	VideoN    int     `json:"videoN"`
	AudioN    int     `json:"audioN"`
	SubtitleN int     `json:"subtitleN"`
	Chapters  int     `json:"chapters"`
}

// IsVertical reports whether the display orientation is portrait.
func (i *Info) IsVertical() bool { return i.DisplayHeight > i.DisplayWidth }

// LongEdge / ShortEdge are display-space helpers used by the resize planner.
func (i *Info) LongEdge() int {
	if i.DisplayWidth > i.DisplayHeight {
		return i.DisplayWidth
	}
	return i.DisplayHeight
}

// ShortEdge returns the shorter display dimension.
func (i *Info) ShortEdge() int {
	if i.DisplayWidth < i.DisplayHeight {
		return i.DisplayWidth
	}
	return i.DisplayHeight
}

// HasVideo reports whether a usable video stream exists.
func (i *Info) HasVideo() bool { return i.Video != nil && i.DisplayWidth > 0 }

// HasAudio reports whether an audio stream exists.
func (i *Info) HasAudio() bool { return i.Audio != nil }

// ---------------------------------------------------------------------------
// ffprobe invocation
// ---------------------------------------------------------------------------

type probeStream struct {
	Index         int               `json:"index"`
	CodecName     string            `json:"codec_name"`
	CodecLongName string            `json:"codec_long_name"`
	CodecType     string            `json:"codec_type"`
	Profile       string            `json:"profile"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	PixFmt        string            `json:"pix_fmt"`
	AvgFrameRate  string            `json:"avg_frame_rate"`
	RFrameRate    string            `json:"r_frame_rate"`
	BitRate       string            `json:"bit_rate"`
	SampleRate    string            `json:"sample_rate"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	Duration      string            `json:"duration"`
	Tags          map[string]string `json:"tags"`
	Disposition   map[string]int    `json:"disposition"`
	SideDataList  []struct {
		SideDataType string `json:"side_data_type"`
		Rotation     int    `json:"rotation"`
	} `json:"side_data_list"`
}

type probeDoc struct {
	Format struct {
		FormatName     string            `json:"format_name"`
		FormatLongName string            `json:"format_long_name"`
		Duration       string            `json:"duration"`
		Size           string            `json:"size"`
		BitRate        string            `json:"bit_rate"`
		Tags           map[string]string `json:"tags"`
	} `json:"format"`
	Streams  []probeStream `json:"streams"`
	Chapters []any         `json:"chapters"`
}

// ProbeFile runs ffprobe against path and returns a normalized description.
// The caller owns the deadline through ctx.
func ProbeFile(ctx context.Context, ffprobePath, path string) (*Info, error) {
	if strings.TrimSpace(ffprobePath) == "" {
		return nil, fmt.Errorf("未配置 ffprobe 路径")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
	}

	args := []string{
		"-hide_banner", "-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters",
		path,
	}
	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	cmd.SysProcAttr = sysx.NoWindow()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffprobe 失败: %s", firstLines(msg, 3))
	}

	var doc probeDoc
	if err := json.Unmarshal([]byte(stdout.String()), &doc); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 输出失败: %w", err)
	}
	return normalize(path, &doc), nil
}

func normalize(path string, doc *probeDoc) *Info {
	info := &Info{
		Path:          path,
		FileName:      filepath.Base(path),
		Ext:           strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Container:     doc.Format.FormatName,
		ContainerLong: doc.Format.FormatLongName,
		Duration:      parseFloat(doc.Format.Duration),
		BitRate:       parseInt(doc.Format.BitRate),
		Chapters:      len(doc.Chapters),
	}
	if st, err := os.Stat(path); err == nil {
		info.Size = st.Size()
	}
	if info.Size == 0 {
		info.Size = parseInt(doc.Format.Size)
	}

	for _, s := range doc.Streams {
		st := Stream{
			Index:         s.Index,
			Type:          s.CodecType,
			Codec:         s.CodecName,
			CodecLong:     s.CodecLongName,
			Profile:       s.Profile,
			Width:         s.Width,
			Height:        s.Height,
			BitRate:       parseInt(s.BitRate),
			PixFmt:        s.PixFmt,
			SampleRate:    int(parseFloat(s.SampleRate)),
			Channels:      s.Channels,
			ChannelLayout: s.ChannelLayout,
		}
		if s.Disposition != nil && s.Disposition["default"] == 1 {
			st.Default = true
		}
		if s.Tags != nil {
			st.Language = s.Tags["language"]
			st.Title = s.Tags["title"]
		}

		switch s.CodecType {
		case "video":
			info.VideoN++
			st.FPS = parseFraction(s.AvgFrameRate)
			if st.FPS <= 0 {
				st.FPS = parseFraction(s.RFrameRate)
			}
			if info.Video == nil && st.Width > 0 {
				info.Video = &st
				info.Width, info.Height = st.Width, st.Height
				info.FPS = st.FPS
				info.PixFmt = st.PixFmt
				info.VideoCodec = st.Codec
				info.Rotation = detectRotation(s, doc.Format.Tags)
			}
		case "audio":
			info.AudioN++
			if info.Audio == nil {
				info.Audio = &st
				info.AudioCodec = st.Codec
			}
		case "subtitle":
			info.SubtitleN++
		}
	}

	// Display dimensions honour the container rotation flag.
	info.DisplayWidth, info.DisplayHeight = info.Width, info.Height
	if info.Rotation == 90 || info.Rotation == 270 {
		info.DisplayWidth, info.DisplayHeight = info.Height, info.Width
	}

	// Fall back to stream bitrate + duration when the container does not report one.
	if info.BitRate <= 0 && info.Duration > 0 && info.Size > 0 {
		info.BitRate = int64(float64(info.Size) * 8 / info.Duration)
	}
	return info
}

// Tags returns the container tags captured by a raw probe document.
func (i *Info) Tags(doc *probeDoc) map[string]string { return doc.Format.Tags }

func detectRotation(s probeStream, containerTags map[string]string) int {
	rot := 0
	for _, sd := range s.SideDataList {
		if sd.Rotation != 0 {
			rot = sd.Rotation
			break
		}
	}
	if rot == 0 && s.Tags != nil {
		if v, ok := s.Tags["rotate"]; ok {
			rot = int(parseFloat(v))
		}
	}
	if rot == 0 && containerTags != nil {
		if v, ok := containerTags["rotate"]; ok {
			rot = int(parseFloat(v))
		}
	}
	rot = ((rot % 360) + 360) % 360
	switch rot {
	case 90, 180, 270:
		return rot
	default:
		return 0
	}
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func parseInt(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseFraction understands both "30000/1001" and plain decimals.
func parseFraction(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0/0" {
		return 0
	}
	if i := strings.IndexByte(s, '/'); i > 0 {
		num := parseFloat(s[:i])
		den := parseFloat(s[i+1:])
		if den == 0 {
			return 0
		}
		return num / den
	}
	return parseFloat(s)
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := lines
	if len(out) > n {
		out = out[:n]
	}
	return strings.Join(out, " / ")
}
