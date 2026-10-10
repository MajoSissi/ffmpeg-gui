package store

import (
	"strings"
)

// GlobalTemplateID is the fixed id of the template that holds the defaults.
const GlobalTemplateID = "t-global"

// GlobalTemplateName is the display name of that template.
const GlobalTemplateName = "全局模板"

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
	Action        string  `json:"action"` // keep | move | copy
	Dir           DirSpec `json:"dir"`
	RenamePattern string  `json:"renamePattern"`
	Overwrite     bool    `json:"overwrite"`
}

// Enabled reports whether any filter rule is active.
func (f FilterSpec) Enabled() bool {
	return f.MinSizeMB > 0 || f.MaxSizeMB > 0 ||
		f.MinLongEdge > 0 || f.MaxLongEdge > 0 ||
		f.MinDuration > 0 || f.MaxDuration > 0 ||
		len(f.IncludeExts) > 0 || len(f.ExcludeExts) > 0
}

// HandlesExcluded reports whether rejected files should be moved or copied.
// There is no second condition on the destination: every DirSpec has a meaning,
// the blank one being "the file's own directory".
func (f FilterSpec) HandlesExcluded() bool {
	return f.Action == ActionMove || f.Action == ActionCopy
}

// ProblemSpec is the policy for files that failed or produced warnings.
//
// ErrorPattern / WarningPattern rename the file on the way out, with the same
// variables as the output section ({name} {ext} {template} {dir}). Empty means
// "keep the original file name". {ext} is the *source* extension here: nothing
// is re-encoded, the original file is the thing being moved.
type ProblemSpec struct {
	ErrorAction    string  `json:"errorAction"` // keep | move | copy
	ErrorDir       DirSpec `json:"errorDir"`
	ErrorPattern   string  `json:"errorPattern,omitempty"`
	WarningAction  string  `json:"warningAction"` // keep | move | copy | mark
	WarningDir     DirSpec `json:"warningDir"`
	WarningPattern string  `json:"warningPattern,omitempty"`
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

// Dir returns the destination rule for the given status.
func (p ProblemSpec) Dir(status string) DirSpec {
	if status == StatusWarning {
		return p.WarningDir
	}
	return p.ErrorDir
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
// DirSpec and rename template behind it.
//
// It acts on the SOURCE file, never on the output. Running the same template over
// the same folder a second time is the case it exists for, and re-encoding files
// that are already done is the thing worth avoiding -- so a repeat pass does not
// reach ffmpeg at all.
type ExistingSpec struct {
	Action    string  `json:"action"` // "" (留在原处) | move | copy
	Dir       DirSpec `json:"dir"`
	Pattern   string  `json:"pattern,omitempty"`
	Overwrite bool    `json:"overwrite"`
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
	}
	t.OutDirSpec = DirSpec{Mode: OutputSibling, Suffix: DefaultOutputSuffix, KeepTree: true}
	// The directory already carries the suffix, so the file name is left alone:
	// /video/mmd/a.mp4 -> /video_out/mmd/a.mp4. Adding "_out" to the name as well
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

	t.Problems = &ProblemSpec{
		ErrorAction:   ActionKeep,
		WarningAction: ActionMark,
	}
	// 三段搬迁的目录一律留空（= 源文件所在目录）。默认动作是「留在原处」和
	//「仅在结果中标记」，所以它们本来就是不写盘的状态；真要去别处，用户自己在
	// 界面上选一种输出方式，比预设一个他没要过的目录更省事。
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
// "Not overridden" is decided per section and nowhere else: the four pointer
// sections use nil, the output section uses OutputOverride. A blank field inside
// an overridden section means that field's own plain default, never the global
// value -- see the note in the body and TestEffectiveBlankOutputFieldsStayBlank.
//
// The returned template shares the global template's section pointers; treat it
// as read-only. Do not call Normalize on it -- that would write through to the
// global template.
func (t Template) Effective(global Template) Template {
	out := t
	if out.Name == "" {
		out.Name = global.Name
	}

	// 输出与命名：整段跟随，段级开关是**唯一**的继承方式。
	//
	// 开关关着时模板自己写的值一律不采纳——否则界面上显示「跟随全局」，实际却用着
	// 模板的旧值，两边说法不一致。
	//
	// 开关打开之后，段内留空的字段**不**再回落全局，而是各归各的最朴素默认：方式留空
	// =与源文件同目录（ResolveDestDir），名称留空=源文件名（ResolveOutput）。两级
	// 「留空即跟随」（段一级 + 字段一级）本身就是同一个意思说了两遍，而字段级的那个
	// 更糟：用户在界面上把「与全局不同」打开了，看到的却还是全局的目录，只能靠一行
	// 提示文字才知道。留空现在只有一个意思——"我就要最普通的那个"。
	if !out.OutputOverride {
		out.OutDirSpec = global.OutDirSpec
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
