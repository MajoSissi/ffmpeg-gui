package store

import (
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Inheritance
// ---------------------------------------------------------------------------

// Turning the output switch on and leaving a field blank means that field's own
// plain default, NOT the global value. Two levels of "blank follows the global
// template" said the same thing twice, and the field-level one was the worse of
// the two: the switch reads 「与全局不同」 while the field silently kept following.
func TestEffectiveBlankOutputFieldsStayBlank(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.OutDirSpec = DirSpec{Mode: OutputCustom, Dir: "D:/Media", KeepTree: true}
	g.OutPattern = "{name}_x"

	// Only the directory is written, so only the directory is taken. The blank
	// pattern stays blank and ResolveOutput reads that as "{name}".
	eff := Template{OutDirSpec: DirSpec{Mode: OutputCustom, Dir: "D:/Other"}, OutputOverride: true}.Effective(g)
	if eff.OutDirSpec.Dir != "D:/Other" || eff.OutDirSpec.KeepTree {
		t.Errorf("override lost: %+v", eff.OutDirSpec)
	}
	if eff.OutPattern != "" {
		t.Errorf("a blank pattern must not pick up the global one: %q", eff.OutPattern)
	}

	// Everything blank stays blank, including KeepTree: the pair is one decision,
	// so a blank expression must not drag the global's structure flag along.
	eff2 := Template{OutputOverride: true, OutPattern: "{name}_y"}.Effective(g)
	if eff2.OutDirSpec != (DirSpec{}) {
		t.Errorf("blank directory inherited the global one: %+v", eff2.OutDirSpec)
	}
	if eff2.OutPattern != "{name}_y" {
		t.Errorf("own pattern lost: %q", eff2.OutPattern)
	}
	eff3 := Template{OutDirSpec: DirSpec{KeepTree: true}, OutputOverride: true}.Effective(g)
	if eff3.OutDirSpec.Mode != "" || eff3.OutDirSpec.Dir != "" {
		t.Errorf("a blank directory must not pick up the global one: %+v", eff3.OutDirSpec)
	}
}

// With the switch off the template's own values are discarded wholesale, even when
// they are set. The editor claims "跟随全局" in that state, so the plan has to agree --
// otherwise saving a template and re-opening it would quietly change the output.
func TestEffectiveFollowerIgnoresOwnOutputValues(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.OutDirSpec = DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}
	g.OutPattern = "{name}.{ext}"

	// A template carrying a stale rule, but following the global one.
	follower := Template{
		OutDirSpec:     DirSpec{Mode: OutputCustom, Dir: "D:/stale", KeepTree: true},
		OutPattern:     "{name}_stale",
		OutputOverride: false,
	}.Effective(g)
	if follower.OutDirSpec != g.OutDirSpec {
		t.Errorf("the directory should come from the global template, got %+v", follower.OutDirSpec)
	}
	if follower.OutPattern != "{name}.{ext}" {
		t.Errorf("stale values survived a follower: %+v", follower)
	}
}

// The four sections are inherited whole, not field by field. A nil section is
// the "follow the global" marker and must stay distinguishable from an empty one.
func TestEffectiveInheritsSectionsWhole(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.Perf.Concurrency = 4
	g.Filter.MinSizeMB = 300
	g.Problems.ErrorAction = ActionMove
	g.Existing = &ExistingSpec{Action: ActionMove}

	// A follower: all four nil.
	follower := Template{Name: "跟随全局"}.Effective(g)
	if follower.Perf != g.Perf || follower.Filter != g.Filter ||
		follower.Problems != g.Problems || follower.Existing != g.Existing {
		t.Error("expected the sections to be taken from the global template")
	}
	if follower.Perf.Concurrency != 4 || follower.Filter.MinSizeMB != 300 {
		t.Errorf("inherited values wrong: %+v %+v", follower.Perf, follower.Filter)
	}
	if follower.Existing.Action != ActionMove {
		t.Errorf("the existing-file policy was not inherited: %+v", follower.Existing)
	}

	// An overrider: only Perf is replaced, the rest follow.
	own := &PerfSpec{Concurrency: 1, LogLevel: "info"}
	partial := Template{Perf: own}.Effective(g)
	if partial.Perf != own {
		t.Error("the template's own section must win")
	}
	if partial.Filter != g.Filter || partial.Filter.MinSizeMB != 300 {
		t.Error("a nil section must still inherit")
	}

	// An explicitly empty section is an override too: 0 means "no limit" and
	// must not be read as "follow".
	empty := &FilterSpec{}
	over := Template{Filter: empty}.Effective(g)
	if over.Filter != empty || over.Filter.MinSizeMB != 0 {
		t.Error("an empty section is an override, not a follower")
	}
}

// Normalize must leave a nil section nil, otherwise saving a follower template
// would silently freeze the global values into it.
func TestNormalizeKeepsNilSectionsNil(t *testing.T) {
	tpl := Template{Name: "x"}
	tpl.Normalize()
	if tpl.Perf != nil || tpl.Filter != nil || tpl.Problems != nil || tpl.Existing != nil {
		t.Error("Normalize materialised a section that was meant to follow the global one")
	}
	if tpl.OutDirSpec.Mode != "" || tpl.OutPattern != "" {
		t.Errorf("Normalize filled an output rule: %+v", tpl.OutDirSpec)
	}
}

// An action the engine does not know must not survive into the file: it would
// reach handleProcessed, match neither move nor copy, and silently behave as
// 「留在原处」 -- the user would think a rule is doing something when it is not.
func TestNormalizeRejectsUnknownExistingAction(t *testing.T) {
	for _, bad := range []string{"delete-everything", "overwrite", "skip", "rename"} {
		tpl := Template{Existing: &ExistingSpec{Action: bad}}
		tpl.Normalize()
		if tpl.Existing.Action != "" {
			t.Errorf("%q survived normalisation: %q", bad, tpl.Existing.Action)
		}
	}
	// The three real ones are untouched.
	for _, ok := range []string{"", ActionMove, ActionCopy} {
		tpl := Template{Existing: &ExistingSpec{Action: ok}}
		tpl.Normalize()
		if tpl.Existing.Action != ok {
			t.Errorf("%q was rewritten to %q", ok, tpl.Existing.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// Global template lifecycle
// ---------------------------------------------------------------------------

func TestEnsureGlobalCreatesAndPins(t *testing.T) {
	list, changed := EnsureGlobal(nil)
	if !changed || len(list) != 1 {
		t.Fatalf("expected a single created global template, got %d (changed=%v)", len(list), changed)
	}
	if !list[0].Global || list[0].ID != GlobalTemplateID {
		t.Errorf("created template is not the global one: %+v", list[0])
	}

	// A list that already has it somewhere else gets it moved to the front.
	list = []Template{{ID: "a", Name: "A"}, {ID: GlobalTemplateID, Name: GlobalTemplateName, Global: true}}
	list, changed = EnsureGlobal(list)
	if !changed || list[0].ID != GlobalTemplateID {
		t.Fatalf("global template was not pinned first: %+v", list)
	}
	// Idempotent: a second pass must report no change so the file is not rewritten.
	if _, changed := EnsureGlobal(list); changed {
		t.Error("EnsureGlobal is not idempotent")
	}
}

// ---------------------------------------------------------------------------
// Seeding
// ---------------------------------------------------------------------------

// A new template must be a copy: writing to it can never reach back into the
// global template through a shared section pointer.
func TestNewFromGlobalIsDeepCopied(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.Perf.Concurrency = 8
	g.Filter.IncludeExts = []string{"mp4"}
	g.Problems.ErrorAction = ActionMove
	// The global ships without this section (see
	// TestDefaultGlobalHasNoExistingSection), so set one up to prove the copy.
	g.Existing = &ExistingSpec{Action: ActionMove, Dir: DirSpec{Mode: OutputCustom, Dir: filepath.Join("D:", "done")}}

	n := NewFromGlobal(g)
	if n.Perf == g.Perf || n.Filter == g.Filter || n.Problems == g.Problems ||
		n.Existing == g.Existing {
		t.Fatal("sections are shared with the global template")
	}
	n.Perf.Concurrency = 1
	n.Filter.IncludeExts[0] = "mkv"
	n.Existing.Dir.Dir = "D:/elsewhere"
	if g.Perf.Concurrency != 8 || g.Filter.IncludeExts[0] != "mp4" {
		t.Error("editing the new template reached back into the global one")
	}
	if g.Existing.Dir.Dir == "D:/elsewhere" {
		t.Error("editing the new template's existing-file section reached back into the global one")
	}
	if n.OutDirSpec.Mode != "" || n.OutPattern != "" || n.OutputOverride {
		// The output section now has its own switch, and a new template starts
		// switched off. Copying the values in would mean a fresh preset is
		// overriding rules the moment it is created.
		t.Errorf("a new template should start following the output section: %+v", n)
	}
	// ...and following must actually resolve to the global values.
	eff := n.Effective(g)
	if eff.OutDirSpec != g.OutDirSpec || eff.OutPattern != g.OutPattern {
		t.Errorf("following did not resolve to the global output rules: %+v", eff)
	}
}

// ---------------------------------------------------------------------------
// The three sections
// ---------------------------------------------------------------------------

func TestFilterSpecEnabled(t *testing.T) {
	if (FilterSpec{Action: ActionMove}).Enabled() {
		t.Error("an action alone must not activate the filter")
	}
	if (FilterSpec{MinSizeMB: 300}).Enabled() != true {
		t.Error("a size limit must activate the filter")
	}
	if (FilterSpec{ExcludeExts: []string{"tmp"}}).Enabled() != true {
		t.Error("an extension list must activate the filter")
	}
}

// HandlesExcluded is what decides whether an excluded file gets relocated, and
// only the action decides it: every directory expression has a meaning, the blank
// one being "the file's own directory".
func TestFilterSpecHandlesExcluded(t *testing.T) {
	cases := []struct {
		name string
		spec FilterSpec
		want bool
	}{
		{"保持原处", FilterSpec{Action: ActionKeep}, false},
		{"没选动作", FilterSpec{}, false},
		{"移动到源目录", FilterSpec{Action: ActionMove}, true},
		{"移动到指定目录", FilterSpec{Action: ActionMove, Dir: DirSpec{Mode: OutputCustom, Dir: "D:/x"}}, true},
		{"复制到镜像目录", FilterSpec{Action: ActionCopy, Dir: DirSpec{Mode: OutputCustom, Dir: "D:/x", KeepTree: true}}, true},
	}
	for _, tc := range cases {
		if got := tc.spec.HandlesExcluded(); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// "mark" is only meaningful for warnings; a "mark" error file is a no-op the UI
// should never offer, so Handles must be false for it.
func TestProblemSpecStatusRouting(t *testing.T) {
	p := ProblemSpec{
		ErrorAction: ActionMove, ErrorDir: DirSpec{Mode: OutputCustom, Dir: "D:/err"},
		WarningAction: ActionMark, WarningDir: DirSpec{Mode: OutputCustom, Dir: "D:/warn"},
	}
	if p.Handles(StatusWarning) {
		t.Error("mark must not relocate a warning file")
	}
	if !p.Handles("error") {
		t.Error("move should relocate an error file")
	}
	if p.Action(StatusWarning) != ActionMark || p.Dir(StatusWarning).Dir != "D:/warn" {
		t.Errorf("warning routing wrong: %q %+v", p.Action(StatusWarning), p.Dir(StatusWarning))
	}
	if p.Action("error") != ActionMove || p.Dir("error").Dir != "D:/err" {
		t.Errorf("error routing wrong: %q %+v", p.Action("error"), p.Dir("error"))
	}
}

func TestPerfSpecNormalizeClamps(t *testing.T) {
	p := &PerfSpec{Concurrency: 99, RetryCount: -3, LogLevel: "LOUD"}
	p.Normalize()
	if p.Concurrency != 16 || p.RetryCount != 0 || p.LogLevel != "warning" {
		t.Errorf("clamping wrong: %+v", p)
	}
	var nilSpec *PerfSpec
	nilSpec.Normalize() // must not panic
}

// A new template copies the audio. Re-encoding it as well is the wrong default:
// most jobs only want to touch the video, and it cannot improve the sound.
func TestNewFromGlobalCopiesAudio(t *testing.T) {
	n := NewFromGlobal(DefaultGlobalTemplate())
	if n.AudioMode != ModeCopy {
		t.Errorf("AudioMode = %q, want %q", n.AudioMode, ModeCopy)
	}
}

// The scale algorithm stays empty when the user picked 默认. Filling lanczos in
// would add :flags=lanczos to the command of a template that never asked for it.
func TestNormalizeKeepsUnsetScaleAlgorithm(t *testing.T) {
	tpl := Template{Resize: ResizeSpec{Mode: ResizeLongEdge, LongEdge: 2560}}
	tpl.Normalize()
	if tpl.Resize.Algorithm != "" {
		t.Errorf("Algorithm = %q, want empty (ffmpeg default)", tpl.Resize.Algorithm)
	}
}

// The 「已处理过的文件」 section skips an already-processed file whenever it is
// present, so shipping one in the global template would silently give every
// template "never process the same file twice". nil is the off state.
func TestDefaultGlobalHasNoExistingSection(t *testing.T) {
	if got := DefaultGlobalTemplate().Existing; got != nil {
		t.Errorf("the global template enables the section by default: %+v", got)
	}
	// A template that opens the section is what opts in.
	eff := Template{Existing: &ExistingSpec{Action: ActionMove}}.Effective(DefaultGlobalTemplate())
	if eff.Existing == nil || eff.Existing.Action != ActionMove {
		t.Errorf("an explicit section must survive Effective: %+v", eff.Existing)
	}
}
