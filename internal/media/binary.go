// Package media wraps the ffmpeg / ffprobe executables: locating them, reading
// their version and probing media files.
package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ffmpeggui/internal/sysx"
)

// Binaries holds the resolved executable paths.
type Binaries struct {
	FFmpeg      string `json:"ffmpeg"`
	FFprobe     string `json:"ffprobe"`
	FFmpegFrom  string `json:"ffmpegFrom"` // "settings" | "path" | "missing"
	FFprobeFrom string `json:"ffprobeFrom"`
}

// Ready reports whether both executables were found.
func (b Binaries) Ready() bool { return b.FFmpeg != "" && b.FFprobe != "" }

func isExecutable(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Resolve locates ffmpeg and ffprobe: explicit settings first, then PATH.
func Resolve(configuredFFmpeg, configuredFFprobe string) Binaries {
	var b Binaries

	if isExecutable(configuredFFmpeg) {
		b.FFmpeg, b.FFmpegFrom = configuredFFmpeg, "settings"
	}
	if isExecutable(configuredFFprobe) {
		b.FFprobe, b.FFprobeFrom = configuredFFprobe, "settings"
	}

	// If only one was configured, look for its sibling in the same folder.
	if b.FFmpeg != "" && b.FFprobe == "" {
		sib := filepath.Join(filepath.Dir(b.FFmpeg), exeName("ffprobe"))
		if isExecutable(sib) {
			b.FFprobe, b.FFprobeFrom = sib, "settings"
		}
	}
	if b.FFprobe != "" && b.FFmpeg == "" {
		sib := filepath.Join(filepath.Dir(b.FFprobe), exeName("ffmpeg"))
		if isExecutable(sib) {
			b.FFmpeg, b.FFmpegFrom = sib, "settings"
		}
	}

	if b.FFmpeg == "" {
		if p, err := exec.LookPath(exeName("ffmpeg")); err == nil {
			b.FFmpeg, b.FFmpegFrom = p, "path"
		} else {
			b.FFmpegFrom = "missing"
		}
	}
	if b.FFprobe == "" {
		if p, err := exec.LookPath(exeName("ffprobe")); err == nil {
			b.FFprobe, b.FFprobeFrom = p, "path"
		} else {
			b.FFprobeFrom = "missing"
		}
	}
	return b
}

func exeName(base string) string {
	if isWindows() {
		return base + ".exe"
	}
	return base
}

// Version runs `-version` and returns the first line plus the configured path.
type ToolInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Source  string `json:"source"`
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
}

// Inspect runs both tools with -version so the settings screen can show what is
// actually about to be executed.
func Inspect(b Binaries) (ToolInfo, ToolInfo) {
	return inspectOne("ffmpeg", b.FFmpeg, b.FFmpegFrom), inspectOne("ffprobe", b.FFprobe, b.FFprobeFrom)
}

func inspectOne(name, path, source string) ToolInfo {
	ti := ToolInfo{Name: name, Path: path, Source: source}
	if path == "" {
		ti.Error = "未找到可执行文件"
		return ti
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "-version")
	cmd.SysProcAttr = sysx.NoWindow()
	out, err := cmd.Output()
	if err != nil {
		ti.Error = err.Error()
		return ti
	}
	line := string(out)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	ti.Version = strings.TrimSpace(line)
	ti.OK = ti.Version != ""
	if !ti.OK {
		ti.Error = "无法解析版本信息"
	}
	return ti
}

// LooksLikeFFmpeg does a cheap sanity check on a user supplied path.
func LooksLikeFFmpeg(path string) error {
	if !isExecutable(path) {
		return fmt.Errorf("路径不存在或不是文件: %s", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	cmd.SysProcAttr = sysx.NoWindow()
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("执行失败: %w", err)
	}
	low := strings.ToLower(string(out))
	if !strings.Contains(low, "ffmpeg version") && !strings.Contains(low, "ffprobe version") {
		return fmt.Errorf("这是一个可执行文件，但不像是 ffmpeg / ffprobe")
	}
	return nil
}

// Encoders parses `ffmpeg -encoders` and returns the set of encoder names the
// current build actually provides. Hardware encoders (NVENC / QSV / AMF / …)
// are only present in some builds, so the UI needs to know before offering them.
func Encoders(ffmpegPath string) (map[string]bool, error) {
	if ffmpegPath == "" {
		return nil, fmt.Errorf("未找到 ffmpeg")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-encoders")
	cmd.SysProcAttr = sysx.NoWindow()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行失败: %w", err)
	}

	found := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		// Rows look like: " V....D h264_nvenc  NVIDIA NVENC H.264 encoder (codec h264)"
		// The six-char flags column is followed by the encoder name; the legend
		// lines (" V..... = Video") must be skipped.
		if len(f) < 2 || len(f[0]) != 6 {
			continue
		}
		if !strings.ContainsAny(f[0][:1], "VAS") || f[1] == "=" {
			continue
		}
		found[f[1]] = true
	}
	return found, nil
}
