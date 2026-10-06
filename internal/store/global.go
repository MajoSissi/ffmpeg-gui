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
			return fmt.Errorf("「%s」选择了指定目录，但目录为空", label)
		}
	case OutputMirror:
		if strings.TrimSpace(d.Dir) == "" {
			return fmt.Errorf("「%s」选择了指定目录（源目录结构），但目录为空", label)
		}
	default:
		return fmt.Errorf("「%s」的输出方式无效：%s", label, d.Mode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The four inheritable sections
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
//
// ErrorPattern / WarningPattern rename the file on the way out, with the same
// variables as the output section ({name} {ext} {template} {dir}). Empty means
// "keep the original file name". {ext} is the *source* extension here: nothing
// is re-encoded, the original file is the thing being moved.
type ProblemSpec struct {
	ErrorAction    string   `json:"errorAction"` // keep | move | copy
	ErrorDest      DestRule `json:"errorDest"`
	ErrorPattern   string   `json:"errorPattern,omitempty"`
	WarningAction  string   `json:"warningAction"` // keep | move | copy | mark
	WarningDest    DestRule `json:"warningDest"`
	WarningPattern string   `json:"warningPattern,omitempty"`
}

// Pattern returns the rename template for the given status, or "" when the file
// should keep its name.
func (p ProblemSpec) Pattern(status string) string {
	if status == StatusWarning {
		return p.WarningPattern
	}
	return p.ErrorPattern
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

// ExistingSpec is the policy for a source file this template has already produced
// output for. It carries exactly the fields as the filter's "被排除文件的处理"
// block, and means exactly the same thing: 留在原处 / 移动 / 复制, with the same
// DestRule and rename template behind it.
//
// It acts on the SOURCE file, never on the output. Running the same template over
// the same folder a second time is the case it exists for, and re-encoding files
// that are already done is the thing worth avoiding -- so a repeat pass does not
// reach ffmpeg at all.
type ExistingSpec struct {
	Action    string   `json:"action"` // "" (留在原处) | move | copy
	Dest      DestRule `json:"dest"`
	Pattern   string   `json:"pattern,omitempty"`
	Overwrite bool     `json:"overwrite"`
}

// Moves reports whether the source file is relocated rather than left where it
// is. The destination has to be usable, or there is nowhere to put it.
func (e ExistingSpec) Moves() bool {
	return (e.Action == ActionMove || e.Action == ActionCopy) && e.Dest.Usable()
}

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
	//
	// The pattern is the file *name*: no {ext} here. The extension comes from the
	// container (or from the source when no container is chosen) and is appended
	// by ResolveOutput, so a naming template never has to spell it out.
	t.OutPattern = "{name}"
	t.OutputOverride = true

	t.Perf = &PerfSpec{Concurrency: 1, LogLevel: "warning", RetryCount: 0}

	// Left nil on purpose. A non-nil section always skips an already-processed
	// file, so shipping one would silently turn every template into "never process
	// the same file twice" -- a behaviour change nobody asked for. nil is the
	// "feature is off" state: a template opts in by opening the section.

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
// when the file has none. It reports whether anything changed so the caller can
// persist.
func EnsureGlobal(list []Template) ([]Template, bool) {
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

	if out.Perf == nil {
		out.Perf = global.Perf
	}
	if out.Existing == nil {
		out.Existing = global.Existing
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

func CloneExisting(e *ExistingSpec) *ExistingSpec {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

// NewFromGlobal seeds a new template from the global template's defaults. The
// output naming is dropped on purpose: it now has its own "与全局不同" switch,
// and a fresh template should start switched off — following the defaults —
// rather than carrying a copy of them that would have to be kept in sync.
func NewFromGlobal(global Template) Template {
	t := Template{ID: NewID(), Name: "", Description: ""}

	// The sections are copied, not shared, so editing the new template does
	// not quietly rewrite the global one.
	t.Perf = ClonePerf(global.Perf)
	t.Existing = CloneExisting(global.Existing)
	t.Filter = CloneFilter(global.Filter)
	t.Problems = CloneProblems(global.Problems)
	// Audio starts on "copy". Most jobs only want to re-encode the video, and a
	// new template that silently re-encodes the audio as well is a worse default
	// than one that leaves it alone: it costs time and cannot improve quality.
	t.AudioMode = ModeCopy
	return t
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
			return "", fmt.Errorf("已选择「指定目录（源目录结构）」，但目录为空")
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
