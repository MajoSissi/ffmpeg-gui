package store

import (
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Output rules
// ---------------------------------------------------------------------------

// OutputDirMode controls where finished files are written. The same four modes
// are offered for the main output, for files rejected by the filter rules and
// for problem files, so all of them behave the same way.
const (
	OutputSame    = "same"    // 与源文件同一目录
	OutputSibling = "sibling" // 同级顶层目录 + 后缀（源目录结构）
	OutputCustom  = "custom"  // 指定目录（源目录结构不保留）
	OutputMirror  = "mirror"  // 指定目录（源目录结构）
)

// DefaultOutputSuffix is appended to the source root directory name in
// "sibling" mode:  /video/mmd  ->  /video/mmd_out
const DefaultOutputSuffix = "_out"

// OnError / OnWarning actions.
const (
	ActionKeep = "keep" // leave the file where it is
	ActionMove = "move" // move it to the destination
	ActionCopy = "copy" // copy it to the destination
	ActionMark = "mark" // only flag it in the result list
)

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// Settings is the application configuration. Everything that describes *what* to
// do now lives in the global template (see global.go); what remains here is
// machine-level setup and UI state.
type Settings struct {
	// --- 二进制与路径 ---
	FFmpegPath     string `json:"ffmpegPath"`
	FFprobePath    string `json:"ffprobePath"`
	GlobalInArgs   string `json:"globalInArgs"`  // 追加到所有命令的输入前参数
	GlobalOutArgs  string `json:"globalOutArgs"` // 追加到所有命令的输入后参数
	HardwareDecode bool   `json:"hardwareDecode"`

	// --- 系统 ---
	PreventSleep   bool `json:"preventSleep"`
	EnableTray     bool `json:"enableTray"`
	CloseToTray    bool `json:"closeToTray"`
	StartMinimized bool `json:"startMinimized"`
	ConfirmExit    bool `json:"confirmExit"`

	// --- 日志 ---
	KeepLogLines int  `json:"keepLogLines"` // 任务日志面板保留的行数（内存）
	SaveRunLog   bool `json:"saveRunLog"`   // 是否把运行日志写入磁盘
	LogMaxSizeMB int  `json:"logMaxSizeMB"` // 单个日志文件的体积上限
	LogKeepDays  int  `json:"logKeepDays"`  // 日志保留天数，超期自动删除

	// --- 界面记忆 ---
	LastTemplateID string `json:"lastTemplateId"`
	ShowLogPanel   bool   `json:"showLogPanel"`
	LogDir         string `json:"logDir"`
	// LogPanelHeight only counts once LogPanelSized is true -- i.e. once the
	// user dragged the grip. Until then the panel takes half the window, no
	// matter what number the file happens to carry: a height saved on another
	// window size would otherwise pin it at a size nobody chose here.
	LogPanelHeight int  `json:"logPanelHeight"`
	LogPanelSized  bool `json:"logPanelSized"`
}

// DefaultSettings returns the shipped configuration.
func DefaultSettings() Settings {
	return Settings{
		PreventSleep: true,
		EnableTray:   true,
		CloseToTray:  true,
		ConfirmExit:  true,

		KeepLogLines: 2000,
		SaveRunLog:   false,
		LogMaxSizeMB: 50,
		LogKeepDays:  7,
		ShowLogPanel: true,
		// 0 + LogPanelSized=false means "half the window", worked out at render
		// time. A pixel default cannot be half of a window it has never seen.
		LogPanelHeight: 0,
		LogPanelSized:  false,
	}
}

// Normalize repairs values that would break the engine.
func (s *Settings) Normalize() {
	if s.KeepLogLines < 100 {
		s.KeepLogLines = 2000
	}
	if s.KeepLogLines > 20000 {
		s.KeepLogLines = 20000
	}
	if s.LogMaxSizeMB < 1 {
		s.LogMaxSizeMB = 50
	}
	if s.LogMaxSizeMB > 4096 {
		s.LogMaxSizeMB = 4096
	}
	if s.LogKeepDays < 1 {
		s.LogKeepDays = 7
	}
	if s.LogKeepDays > 3650 {
		s.LogKeepDays = 3650
	}
	if s.LogPanelHeight < 0 {
		s.LogPanelHeight = 0
	}
	if s.LogPanelHeight > 800 {
		s.LogPanelHeight = 800
	}
	if s.LogDir == "" {
		s.LogDir = filepath.Join(DataDir(), "logs")
	}
}

func normalizeExts(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		e = strings.TrimPrefix(e, ".")
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// SettingsPath is the settings file location.
func SettingsPath() string { return Path("settings.json") }

// LoadSettings reads settings.json, falling back to defaults.
func LoadSettings() Settings {
	s := DefaultSettings()
	if ok, err := ReadJSON(SettingsPath(), &s); !ok || err != nil {
		_ = WriteJSON(SettingsPath(), s)
	}
	s.Normalize()
	return s
}

// SaveSettings writes settings.json.
func SaveSettings(s Settings) error {
	s.Normalize()
	return WriteJSON(SettingsPath(), s)
}
