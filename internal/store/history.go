package store

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// MediaSummary is a flat snapshot of a media file used in history records.
type MediaSummary struct {
	Exists     bool    `json:"exists"`
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	Container  string  `json:"container"`
	Duration   float64 `json:"duration"`
	VideoCodec string  `json:"videoCodec"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	PixFmt     string  `json:"pixFmt"`
	AudioCodec string  `json:"audioCodec"`
	SampleRate int     `json:"sampleRate"`
	Channels   int     `json:"channels"`
	BitRate    int64   `json:"bitRate"`
}

// Record is one processed (or rejected) file kept in history.json.
type Record struct {
	ID         string `json:"id"`
	Input      string `json:"input"`
	Output     string `json:"output"`
	TemplateID string `json:"templateId"`

	// SourceMovedTo is where 「已处理过的文件」 filed the source, when that rule is
	// set to 移动. Input is the path it was processed from, and after a move that
	// path is empty on disk -- so this is the only thing that can still take the
	// user to the file.
	SourceMovedTo string `json:"sourceMovedTo"`

	TemplateName string       `json:"templateName"`
	Command      string       `json:"command"`
	Status       string       `json:"status"`
	Note         string       `json:"note"`
	Error        string       `json:"error"`
	Warnings     []string     `json:"warnings"`
	StartedAt    time.Time    `json:"startedAt"`
	EndedAt      time.Time    `json:"endedAt"`
	ElapsedMS    int64        `json:"elapsedMs"`
	Speed        float64      `json:"speed"`
	Before       MediaSummary `json:"before"`
	After        MediaSummary `json:"after"`
}

// Ratio is the output/input size ratio; 0 when not comparable.
func (r Record) Ratio() float64 {
	if r.Before.Size <= 0 || r.After.Size <= 0 {
		return 0
	}
	return float64(r.After.Size) / float64(r.Before.Size)
}

// Saved reports how many bytes were saved (may be negative).
func (r Record) Saved() int64 { return r.Before.Size - r.After.Size }

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

const historyLimit = 20000

// HistoryPath is the history file location.
func HistoryPath() string { return Path("history.json") }

// LoadHistory reads history.json (newest first).
func LoadHistory() []Record {
	var list []Record
	if _, err := ReadJSON(HistoryPath(), &list); err != nil {
		return nil
	}
	return list
}

// SaveHistory writes history.json, trimming to historyLimit entries.
func SaveHistory(list []Record) error {
	if len(list) > historyLimit {
		list = list[:historyLimit]
	}
	return WriteJSON(HistoryPath(), list)
}

// ---------------------------------------------------------------------------
// CSV export
// ---------------------------------------------------------------------------

func ratioText(r Record) string {
	v := r.Ratio()
	if v <= 0 {
		return ""
	}
	return strconv.FormatFloat(v*100, 'f', 1, 64) + "%"
}

func resolution(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", w, h)
}

func mb(n int64) string {
	if n <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 2, 64)
}

func dur(sec float64) string {
	if sec <= 0 {
		return ""
	}
	d := time.Duration(sec * float64(time.Second))
	return d.Round(time.Millisecond).String()
}

func tm(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func joinList(s []string) string {
	if len(s) == 0 {
		return ""
	}
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " | "
		}
		out += v
	}
	return out
}

// ExportCSV writes records to path as UTF-8 CSV (with BOM so Excel opens it
// with the correct encoding).
func ExportCSV(path string, list []Record) error {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")

	w := csv.NewWriter(&buf)
	header := []string{
		"状态", "文件名", "源路径", "输出路径", "模板",
		"处理前格式", "处理前分辨率", "处理前大小(MB)", "处理前时长", "处理前码率(kbps)", "处理前视频/音频",
		"处理后格式", "处理后分辨率", "处理后大小(MB)", "处理后时长", "处理后码率(kbps)", "处理后视频/音频",
		"压缩比", "节省(MB)", "平均速度", "耗时", "开始时间", "结束时间", "备注", "警告", "错误", "命令",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range list {
		sizeChange := ""
		if r.Before.Size > 0 && r.After.Size > 0 {
			sizeChange = strconv.FormatFloat(float64(r.Saved())/(1024*1024), 'f', 2, 64)
		}
		row := []string{
			r.Status,
			filepath.Base(r.Input),
			r.Input,
			r.Output,
			r.TemplateName,
			r.Before.Container,
			resolution(r.Before.Width, r.Before.Height),
			mb(r.Before.Size),
			dur(r.Before.Duration),
			bitrateText(r.Before.BitRate),
			codecPair(r.Before),
			r.After.Container,
			resolution(r.After.Width, r.After.Height),
			mb(r.After.Size),
			dur(r.After.Duration),
			bitrateText(r.After.BitRate),
			codecPair(r.After),
			ratioText(r),
			sizeChange,
			speedText(r.Speed),
			dur(float64(r.ElapsedMS) / 1000),
			tm(r.StartedAt),
			tm(r.EndedAt),
			r.Note,
			joinList(r.Warnings),
			r.Error,
			r.Command,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func bitrateText(b int64) string {
	if b <= 0 {
		return ""
	}
	return strconv.FormatInt(b/1000, 10)
}

func speedText(s float64) string {
	if s <= 0 {
		return ""
	}
	return strconv.FormatFloat(s, 'f', 2, 64) + "x"
}

func codecPair(m MediaSummary) string {
	switch {
	case m.VideoCodec != "" && m.AudioCodec != "":
		return m.VideoCodec + " / " + m.AudioCodec
	case m.VideoCodec != "":
		return m.VideoCodec
	default:
		return m.AudioCodec
	}
}
