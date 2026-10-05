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
	OutputSibling = "sibling" // 同级目录 + 后缀，保持子目录结构
	OutputCustom  = "custom"  // 全部输出到指定目录（不保留子目录）
	OutputMirror  = "mirror"  // 输出到指定目录并保持子目录结构
)

// DefaultOutputSuffix is appended to the source root directory name in
// "sibling" mode:  /video/mmd  ->  /video/mmd_out
const DefaultOutputSuffix = "_out"

// Conflict policies when the target file already exists.
const (
	ConflictOverwrite = "overwrite"
	ConflictSkip      = "skip"
	ConflictRename    = "rename"
)

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
	LogPanelHeight int    `json:"logPanelHeight"`
	LogDir         string `json:"logDir"`

	// --- 队列级筛选覆盖 ---
	// 任务页可以临时改筛选条件，优先级高于模板与全局模板（见 engine.Runner 的
	// queueFilter）。nil = 不覆盖，沿用模板的值；这是"跟随"与"刻意清空"必须能
	// 区分开的原因，所以用指针而不是靠 Enabled() 猜。
	QueueFilter *FilterSpec `json:"queueFilter,omitempty"`
	// QueueFilterSet 记录用户是否显式打开了队列筛选面板。关掉面板不是"清空条件"，
	// 而是不参与判断；两者混在一起会让用户关掉面板后筛选悄悄失效。
	QueueFilterSet bool `json:"queueFilterSet,omitempty"`

	// --- 迁移来源（读一次即清空） ---
	// 这些字段曾经是全局配置，现已并入「全局模板」。它们沿用旧的扁平/嵌套 JSON
	// 键，是为了让旧的 settings.json 仍能被解析：App 在第一次发现模板列表里没有
	// 全局模板时，会用它们建一个（NewGlobalFromLegacy），随后 ClearLegacy 把指针
	// 置空；配合 omitempty，下次保存起它们就不再出现在文件里。
	LegacyOutputDirMode   string `json:"outputDirMode,omitempty"`
	LegacyOutputDir       string `json:"outputDir,omitempty"`
	LegacyOutputSuffix    string `json:"outputSuffix,omitempty"`
	LegacyNamePattern     string `json:"namePattern,omitempty"`
	LegacyConflict        string `json:"conflict,omitempty"`
	LegacyConcurrency     int    `json:"concurrency,omitempty"`
	LegacyLogLevel        string `json:"logLevel,omitempty"`
	LegacyRetryCount      int    `json:"retryCount,omitempty"`
	LegacyDeleteOnFail    bool   `json:"deleteOnFail,omitempty"`
	LegacyOnErrorAction   string `json:"onErrorAction,omitempty"`
	LegacyOnErrorDir      string `json:"onErrorDir,omitempty"`
	LegacyOnWarningAction string `json:"onWarningAction,omitempty"`
	LegacyOnWarningDir    string `json:"onWarningDir,omitempty"`

	// LegacyFilters 必须是嵌套结构：旧文件里 filters 就是一个对象，用点号键
	// （`json:"filters.minSizeMB"`）encoding/json 并不支持，会静默读不到。
	// 用指针 + omitempty 才能在清空后从文件里彻底消失。
	LegacyFilters *LegacyFilterRules `json:"filters,omitempty"`
}

// LegacyFilterRules mirrors the filter block of the pre-global-template layout.
type LegacyFilterRules struct {
	MinSizeMB     float64  `json:"minSizeMB"`
	MaxSizeMB     float64  `json:"maxSizeMB"`
	MinLongEdge   int      `json:"minLongEdge"`
	MaxLongEdge   int      `json:"maxLongEdge"`
	MinDuration   float64  `json:"minDuration"`
	MaxDuration   float64  `json:"maxDuration"`
	IncludeExts   []string `json:"includeExts"`
	ExcludeExts   []string `json:"excludeExts"`
	Action        string   `json:"action"`
	TargetDir     string   `json:"targetDir"`
	RenamePattern string   `json:"renamePattern"`
	MirrorTree    bool     `json:"mirrorTree"`
	Overwrite     bool     `json:"overwrite"`
}

// HasLegacy reports whether an older settings.json still carries the values that
// have since moved into the global template.
func (s Settings) HasLegacy() bool {
	if s.LegacyOutputDirMode != "" || s.LegacyOutputDir != "" ||
		s.LegacyOutputSuffix != "" || s.LegacyNamePattern != "" || s.LegacyConflict != "" ||
		s.LegacyConcurrency > 0 || s.LegacyLogLevel != "" || s.LegacyRetryCount > 0 ||
		s.LegacyDeleteOnFail || s.LegacyOnErrorAction != "" || s.LegacyOnErrorDir != "" ||
		s.LegacyOnWarningAction != "" || s.LegacyOnWarningDir != "" {
		return true
	}
	f := s.LegacyFilters
	if f == nil {
		return false
	}
	return f.MinSizeMB > 0 || f.MaxSizeMB > 0 ||
		f.MinLongEdge > 0 || f.MaxLongEdge > 0 ||
		f.MinDuration > 0 || f.MaxDuration > 0 ||
		f.Action != "" || f.TargetDir != "" || f.RenamePattern != "" ||
		f.MirrorTree || f.Overwrite ||
		len(f.IncludeExts) > 0 || len(f.ExcludeExts) > 0
}

// DefaultSettings returns the shipped configuration.
func DefaultSettings() Settings {
	return Settings{
		PreventSleep: true,
		EnableTray:   true,
		CloseToTray:  true,
		ConfirmExit:  true,

		KeepLogLines:   2000,
		SaveRunLog:     false,
		LogMaxSizeMB:   50,
		LogKeepDays:    7,
		ShowLogPanel:   true,
		LogPanelHeight: 200,
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
	if s.LogPanelHeight < 120 {
		s.LogPanelHeight = 200
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
