package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// GlobalTemplateID is the fixed id of the template that holds the defaults.
const GlobalTemplateID = "t-global"

// GlobalTemplateName is the display name of that template.
const GlobalTemplateName = "全局模板"

// ---------------------------------------------------------------------------
// DestRule — 每一段产物都能自己决定"放到哪"
// ---------------------------------------------------------------------------

// DestRule says where one stage writes its files. The main output, the files
// rejected by the filter rules and the problem files (failed / warning) all use
// it, so every stage offers the same four choices instead of one hard-coded
// directory. A blank field inherits from the fallback rule.
type DestRule struct {
	Mode   string `json:"mode"`   // "" | same | sibling | custom | mirror
	Dir    string `json:"dir"`    // custom / mirror 使用的根目录
	Suffix string `json:"suffix"` // sibling 使用的后缀，如 _out
}

// Inherit fills every blank field from fb.
func (d DestRule) Inherit(fb DestRule) DestRule {
	if strings.TrimSpace(d.Mode) == "" {
		d.Mode = fb.Mode
	}
	if strings.TrimSpace(d.Dir) == "" {
		d.Dir = fb.Dir
	}
	if strings.TrimSpace(d.Suffix) == "" {
		d.Suffix = fb.Suffix
	}
	return d
}

// Usable reports whether the rule points at a real directory. Modes other than
// "same" and "sibling" need Dir; an empty one is a configuration mistake worth
// reporting rather than silently writing next to the source.
func (d DestRule) Usable() bool {
	switch d.Mode {
	case OutputCustom, OutputMirror:
		return strings.TrimSpace(d.Dir) != ""
	default:
		return true
	}
}

// Validate returns a user-facing error when the rule cannot be used.
func (d DestRule) Validate(label string) error {
	switch d.Mode {
	case "", OutputSame, OutputSibling:
		return nil
	case OutputCustom:
		if strings.TrimSpace(d.Dir) == "" {
			return fmt.Errorf("「%s」选择了全部输出到指定目录，但目录为空", label)
		}
	case OutputMirror:
		if strings.TrimSpace(d.Dir) == "" {
			return fmt.Errorf("「%s」选择了指定目录 + 保持子目录结构，但目录为空", label)
		}
	default:
		return fmt.Errorf("「%s」的输出方式无效：%s", label, d.Mode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The three inheritable sections
// ---------------------------------------------------------------------------

// PerfSpec is the processing-performance section. A template that leaves it nil
// uses the global template's values.
type PerfSpec struct {
	Concurrency  int    `json:"concurrency"`
	LogLevel     string `json:"logLevel"`
	RetryCount   int    `json:"retryCount"`
	Threads      int    `json:"threads"`
	IdlePriority bool   `json:"idlePriority"`
	DeleteOnFail bool   `json:"deleteOnFail"`
}

// Normalize clamps the values into ranges the engine accepts.
func (p *PerfSpec) Normalize() {
	if p == nil {
		return
	}
	if p.Concurrency < 1 {
		p.Concurrency = 1
	}
	if p.Concurrency > 16 {
		p.Concurrency = 16
	}
	if p.RetryCount < 0 {
		p.RetryCount = 0
	}
	if p.RetryCount > 5 {
		p.RetryCount = 5
	}
	// 0 means "not set": ffmpeg then picks its own thread count, which is the
	// sensible default. Asking for more cores than the machine has does not make
	// encoding faster, so the ceiling is high but real.
	if p.Threads < 0 {
		p.Threads = 0
	}
	if p.Threads > 64 {
		p.Threads = 64
	}
	switch strings.ToLower(p.LogLevel) {
	case "quiet", "panic", "fatal", "error", "warning", "info", "verbose", "debug":
		p.LogLevel = strings.ToLower(p.LogLevel)
	default:
		p.LogLevel = "warning"
	}
}

// FilterSpec decides which inputs get processed and what happens to the rest.
// A template that leaves it nil uses the global template's rules.
type FilterSpec struct {
	MinSizeMB   float64  `json:"minSizeMB"`
	MaxSizeMB   float64  `json:"maxSizeMB"`
	MinLongEdge int      `json:"minLongEdge"`
	MaxLongEdge int      `json:"maxLongEdge"`
	MinDuration float64  `json:"minDuration"`
	MaxDuration float64  `json:"maxDuration"`
	IncludeExts []string `json:"includeExts"`
	ExcludeExts []string `json:"excludeExts"`

	// What to do with rejected files.
	Action        string   `json:"action"` // keep | move | copy
	Dest          DestRule `json:"dest"`
	RenamePattern string   `json:"renamePattern"`
	Overwrite     bool     `json:"overwrite"`
}

// Enabled reports whether any filter rule is active.
func (f FilterSpec) Enabled() bool {
	return f.MinSizeMB > 0 || f.MaxSizeMB > 0 ||
		f.MinLongEdge > 0 || f.MaxLongEdge > 0 ||
		f.MinDuration > 0 || f.MaxDuration > 0 ||
		len(f.IncludeExts) > 0 || len(f.ExcludeExts) > 0
}

// HandlesExcluded reports whether rejected files should be moved or copied.
func (f FilterSpec) HandlesExcluded() bool {
	return (f.Action == ActionMove || f.Action == ActionCopy) && f.Dest.Usable()
}

// ProblemSpec is the policy for files that failed or produced warnings.
type ProblemSpec struct {
	ErrorAction   string   `json:"errorAction"` // keep | move | copy
	ErrorDest     DestRule `json:"errorDest"`
	WarningAction string   `json:"warningAction"` // keep | move | copy | mark
	WarningDest   DestRule `json:"warningDest"`
}

// Handles reports whether problem files of the given status should be relocated.
func (p ProblemSpec) Handles(status string) bool {
	if status == StatusWarning {
		return p.WarningAction == ActionMove || p.WarningAction == ActionCopy
	}
	return p.ErrorAction == ActionMove || p.ErrorAction == ActionCopy
}

// Dest returns the destination rule for the given status.
func (p ProblemSpec) Dest(status string) DestRule {
	if status == StatusWarning {
		return p.WarningDest
	}
	return p.ErrorDest
}

// Action returns the action for the given status.
func (p ProblemSpec) Action(status string) string {
	if status == StatusWarning {
		return p.WarningAction
	}
	return p.ErrorAction
}

// StatusWarning mirrors engine.StatusWarning without importing the engine.
const StatusWarning = "warning"

// ---------------------------------------------------------------------------
// Global template
// ---------------------------------------------------------------------------

// DefaultGlobalTemplate returns the shipped global template: the values the app
// shipped with before templates could override anything.
func DefaultGlobalTemplate() Template {
	t := Template{
		ID:          GlobalTemplateID,
		Name:        GlobalTemplateName,
		Description: "所有模板的默认值。新建模板会以它为起点；模板里留空的项也跟随它。",
		Global:      true,
		UpdatedAt:   0,
	}
	t.OutMode = OutputSibling
	t.OutSuffix = DefaultOutputSuffix
	// The directory already carries the suffix, so the file name is left alone:
	// /video/mmd/a.mp4 -> /video/mmd_out/a.mp4. Adding "_out" to the name as well
	// would just be noise on top of the new directory.
	t.OutPattern = "{name}.{ext}"
	t.OutConflict = ConflictRename
	t.OutputOverride = true

	t.Perf = &PerfSpec{Concurrency: 1, LogLevel: "warning", RetryCount: 0}

	t.Filter = &FilterSpec{Action: ActionKeep}
	t.Filter.Dest = DestRule{Mode: OutputCustom}

	t.Problems = &ProblemSpec{
		ErrorAction:   ActionKeep,
		WarningAction: ActionMark,
	}
	t.Problems.ErrorDest = DestRule{Mode: OutputCustom}
	t.Problems.WarningDest = DestRule{Mode: OutputMirror}
	return t
}

// FindGlobal returns the global template from a list, or nil when it is missing.
func FindGlobal(list []Template) *Template {
	for i := range list {
		if list[i].Global {
			return &list[i]
		}
	}
	return nil
}

// GlobalOrDefault returns the global template from a list, falling back to the
// shipped defaults so callers never have to nil-check.
func GlobalOrDefault(list []Template) Template {
	if g := FindGlobal(list); g != nil {
		return *g
	}
	return DefaultGlobalTemplate()
}

// EnsureGlobal guarantees the list starts with a global template, creating one
// from migrate when the file predates this feature. It reports whether anything
// changed so the caller can persist.
func EnsureGlobal(list []Template, migrate func() Template) ([]Template, bool) {
	for i := range list {
		if list[i].Global {
			if i == 0 {
				return list, false
			}
			// Keep it pinned first so the UI can rely on the order.
			g := list[i]
			list = append(list[:i], list[i+1:]...)
			out := append([]Template{g}, list...)
			g.Normalize()
			return out, true
		}
	}
	g := DefaultGlobalTemplate()
	if migrate != nil {
		g = migrate()
		g.ID = GlobalTemplateID
		g.Global = true
		g.Builtin = false
		if strings.TrimSpace(g.Name) == "" {
			g.Name = GlobalTemplateName
		}
	}
	g.Normalize()
	return append([]Template{g}, list...), true
}

// Effective merges a template with the global template. Every section the
// template does not override is taken from the global one, so a template only
// has to describe what makes it different.
//
// The returned template shares the global template's section pointers; treat it
// as read-only. Do not call Normalize on it -- that would write through to the
// global template.
func (t Template) Effective(global Template) Template {
	out := t
	if out.Name == "" {
		out.Name = global.Name
	}

	// 输出与命名：整段跟随。开关关着时模板自己写的值一律不采纳——否则界面上
	// 显示「跟随全局」，实际却用着模板的旧值，两边说法不一致。
	if !out.OutputOverride {
		out.OutMode = ""
		out.OutDir = ""
		out.OutSuffix = ""
		out.OutPattern = ""
		out.OutConflict = ""
	}
	// 段内再逐字段留空即回落：打开开关但没改的字段跟全局等价。
	if strings.TrimSpace(out.OutMode) == "" {
		out.OutMode = global.OutMode
	}
	if strings.TrimSpace(out.OutDir) == "" {
		out.OutDir = global.OutDir
	}
	if strings.TrimSpace(out.OutSuffix) == "" {
		out.OutSuffix = global.OutSuffix
	}
	if strings.TrimSpace(out.OutPattern) == "" {
		out.OutPattern = global.OutPattern
	}
	if strings.TrimSpace(out.OutConflict) == "" {
		out.OutConflict = global.OutConflict
	}

	if out.Perf == nil {
		out.Perf = global.Perf
	}
	if out.Filter == nil {
		out.Filter = global.Filter
	}
	if out.Problems == nil {
		out.Problems = global.Problems
	}
	return out
}

// ClonePerf / CloneFilter / CloneProblems deep-copy the inheritable sections.
// Duplicating a template needs them: without a copy the duplicate would share
// the section pointers and editing it would silently rewrite the original.
func ClonePerf(p *PerfSpec) *PerfSpec {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

func CloneFilter(f *FilterSpec) *FilterSpec {
	if f == nil {
		return nil
	}
	c := *f
	c.IncludeExts = append([]string(nil), f.IncludeExts...)
	c.ExcludeExts = append([]string(nil), f.ExcludeExts...)
	return &c
}

func CloneProblems(p *ProblemSpec) *ProblemSpec {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// NewFromGlobal seeds a new template from the global template's defaults. The
// output naming is dropped on purpose: it now has its own "与全局不同" switch,
// and a fresh template should start switched off — following the defaults —
// rather than carrying a copy of them that would have to be kept in sync.
func NewFromGlobal(global Template) Template {
	t := Template{ID: NewID(), Name: "", Description: ""}

	// The three sections are copied, not shared, so editing the new template does
	// not quietly rewrite the global one.
	t.Perf = ClonePerf(global.Perf)
	t.Filter = CloneFilter(global.Filter)
	t.Problems = CloneProblems(global.Problems)
	return t
}

// ---------------------------------------------------------------------------
// Migration from the pre-global-template settings layout
// ---------------------------------------------------------------------------

// LegacySettings is the set of settings that used to be global and now live in
// the global template. It exists purely so an existing settings.json can be read
// once and moved; NewGlobalFromLegacy reports whether there was anything to
// move, and the caller then clears the legacy fields.
func NewGlobalFromLegacy(s Settings) (Template, bool) {
	g := DefaultGlobalTemplate()
	moved := false

	pick := func(cond bool) {
		if cond {
			moved = true
		}
	}

	// 输出与命名
	if s.LegacyOutputDirMode != "" {
		g.OutMode = s.LegacyOutputDirMode
	}
	pick(s.LegacyOutputDir != "" || s.LegacyOutputSuffix != "" ||
		s.LegacyNamePattern != "" || s.LegacyConflict != "")
	if s.LegacyOutputDir != "" {
		g.OutDir = s.LegacyOutputDir
	}
	if s.LegacyOutputSuffix != "" {
		g.OutSuffix = s.LegacyOutputSuffix
	}
	if s.LegacyNamePattern != "" {
		g.OutPattern = s.LegacyNamePattern
	}
	if s.LegacyConflict != "" {
		g.OutConflict = s.LegacyConflict
	}

	// 处理性能
	pick(s.LegacyConcurrency > 0 || s.LegacyLogLevel != "" ||
		s.LegacyRetryCount > 0 || s.LegacyDeleteOnFail)
	if g.Perf == nil {
		g.Perf = &PerfSpec{}
	}
	if s.LegacyConcurrency > 0 {
		g.Perf.Concurrency = s.LegacyConcurrency
	}
	if s.LegacyLogLevel != "" {
		g.Perf.LogLevel = s.LegacyLogLevel
	}
	g.Perf.RetryCount = s.LegacyRetryCount
	g.Perf.DeleteOnFail = s.LegacyDeleteOnFail

	// 筛选与转移：TargetDir + MirrorTree 折叠成一个新的 DestRule。
	f := s.LegacyFilters
	if f != nil {
		if f.Action != "" {
			g.Filter.Action = f.Action
		}
		pick(f.TargetDir != "" || f.MirrorTree || f.RenamePattern != "" || f.Overwrite)
		if f.TargetDir != "" {
			mode := OutputCustom
			if f.MirrorTree {
				mode = OutputMirror
			}
			g.Filter.Dest = DestRule{Mode: mode, Dir: f.TargetDir, Suffix: DefaultOutputSuffix}
		}
		g.Filter.RenamePattern = f.RenamePattern
		g.Filter.Overwrite = f.Overwrite
		g.Filter.MinSizeMB = f.MinSizeMB
		g.Filter.MaxSizeMB = f.MaxSizeMB
		g.Filter.MinDuration = f.MinDuration
		g.Filter.MaxDuration = f.MaxDuration
		g.Filter.MinLongEdge = f.MinLongEdge
		g.Filter.MaxLongEdge = f.MaxLongEdge
		g.Filter.IncludeExts = normalizeExts(f.IncludeExts)
		g.Filter.ExcludeExts = normalizeExts(f.ExcludeExts)
	}

	// 错误与警告：旧实现固定按镜像目录处理，所以迁移时保持 mirror。
	if s.LegacyOnErrorAction != "" {
		g.Problems.ErrorAction = s.LegacyOnErrorAction
	}
	if s.LegacyOnWarningAction != "" {
		g.Problems.WarningAction = s.LegacyOnWarningAction
	}
	pick(s.LegacyOnErrorDir != "" || s.LegacyOnWarningDir != "")
	if s.LegacyOnErrorDir != "" {
		g.Problems.ErrorDest = DestRule{Mode: OutputMirror, Dir: s.LegacyOnErrorDir, Suffix: DefaultOutputSuffix}
	}
	if s.LegacyOnWarningDir != "" {
		g.Problems.WarningDest = DestRule{Mode: OutputMirror, Dir: s.LegacyOnWarningDir, Suffix: DefaultOutputSuffix}
	}

	return g, moved
}

// ClearLegacy drops the migrated settings so the next save does not write them
// back into settings.json.
func (s *Settings) ClearLegacy() {
	s.LegacyOutputDirMode = ""
	s.LegacyOutputDir = ""
	s.LegacyOutputSuffix = ""
	s.LegacyNamePattern = ""
	s.LegacyConflict = ""
	s.LegacyConcurrency = 0
	s.LegacyLogLevel = ""
	s.LegacyRetryCount = 0
	s.LegacyDeleteOnFail = false
	s.LegacyOnErrorAction = ""
	s.LegacyOnErrorDir = ""
	s.LegacyOnWarningAction = ""
	s.LegacyOnWarningDir = ""
	s.LegacyFilters = nil
}

// ---------------------------------------------------------------------------
// Dest resolution shared by every stage
// ---------------------------------------------------------------------------

// DestRequest asks where a file should land under a rule.
type DestRequest struct {
	Rule    DestRule
	SrcPath string
	// SrcRoot is the directory the user added; it anchors "sibling" and "mirror".
	SrcRoot string
	// DefaultSuffix is used when neither the rule nor the fallback names one.
	DefaultSuffix string
}

// ResolveDestDir returns the directory a file should be written to, honouring
// the same four modes everywhere.
func ResolveDestDir(req DestRequest) (string, error) {
	if err := req.Rule.Validate("输出"); err != nil {
		return "", err
	}
	srcDir := filepath.Dir(req.SrcPath)
	suffix := strings.TrimSpace(req.Rule.Suffix)
	if suffix == "" {
		suffix = strings.TrimSpace(req.DefaultSuffix)
	}
	if suffix == "" {
		suffix = DefaultOutputSuffix
	}

	// subDir returns the path of srcDir relative to the added root, or "" when
	// the file sits directly in the root.
	subDir := func() string {
		if req.SrcRoot == "" {
			return ""
		}
		rel, err := filepath.Rel(req.SrcRoot, srcDir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return ""
		}
		return rel
	}

	switch req.Rule.Mode {
	case OutputCustom:
		return strings.TrimSpace(req.Rule.Dir), nil
	case OutputMirror:
		root := strings.TrimSpace(req.Rule.Dir)
		if root == "" {
			return "", fmt.Errorf("已选择「指定目录 + 保持子目录结构」，但目录为空")
		}
		if rel := subDir(); rel != "" {
			return filepath.Join(root, rel), nil
		}
		return root, nil
	case OutputSibling:
		// Sibling of the folder that was added, sub-tree preserved:
		//   /video/mmd/a.mp4      -> /video/mmd_out/a.mp4
		//   /video/mmd/sub/b.mp4  -> /video/mmd_out/sub/b.mp4
		// A sibling (rather than "next to the source") also keeps results outside
		// the scanned tree, so re-running never picks them up again.
		outRoot := srcDir + suffix
		if req.SrcRoot != "" {
			outRoot = req.SrcRoot + suffix
		}
		if rel := subDir(); rel != "" {
			return filepath.Join(outRoot, rel), nil
		}
		return outRoot, nil
	default: // OutputSame and the empty mode
		return srcDir, nil
	}
}
