package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Inheritance
// ---------------------------------------------------------------------------

// A template whose output switch is on but leaves fields blank picks up the global
// ones field by field -- that is the whole point of "留空即跟随".
func TestEffectiveInheritsOutputFieldByField(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.OutMode = OutputMirror
	g.OutDir = filepath.Join("D:", "Media")
	g.OutSuffix = "_done"
	g.OutPattern = "{name}_x"
	g.OutConflict = ConflictSkip

	eff := Template{OutputOverride: true}.Effective(g)
	if eff.OutMode != OutputMirror || eff.OutDir != g.OutDir ||
		eff.OutSuffix != "_done" || eff.OutPattern != "{name}_x" || eff.OutConflict != ConflictSkip {
		t.Fatalf("blank template did not inherit: %+v", eff)
	}

	// One override must not drag the rest along: OutSuffix stays global.
	eff2 := Template{OutSuffix: "_custom", OutputOverride: true}.Effective(g)
	if eff2.OutSuffix != "_custom" {
		t.Errorf("override lost: %q", eff2.OutSuffix)
	}
	if eff2.OutMode != OutputMirror || eff2.OutPattern != "{name}_x" {
		t.Errorf("override leaked into the other fields: %+v", eff2)
	}
}

// With the switch off the template's own values are discarded wholesale, even when
// they are set. The editor claims "跟随全局" in that state, so the plan has to agree --
// otherwise saving a template and re-opening it would quietly change the output.
func TestEffectiveFollowerIgnoresOwnOutputValues(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.OutMode = OutputSibling
	g.OutSuffix = "_out"
	g.OutPattern = "{name}.{ext}"

	// A template carrying a stale sibling rule, but following the global one.
	follower := Template{
		OutMode: OutputSame, OutSuffix: "_stale", OutPattern: "{name}_stale",
		OutputOverride: false,
	}.Effective(g)
	if follower.OutMode != OutputSibling {
		t.Errorf("OutMode should come from the global template, got %q", follower.OutMode)
	}
	if follower.OutSuffix != "_out" || follower.OutPattern != "{name}.{ext}" {
		t.Errorf("stale values survived a follower: %+v", follower)
	}
}

// Files written before the switch existed carry output values but no flag. They must
// keep behaving the way they did instead of silently reverting to the global rules.
func TestNormalizeAdoptsPreSwitchOutputValues(t *testing.T) {
	legacy := Template{Name: "旧模板", OutMode: OutputSame, OutSuffix: "_old"}
	legacy.Normalize()
	if !legacy.OutputOverride {
		t.Error("a template with its own output values must be treated as overriding them")
	}
	if !legacy.OverridesOutput() {
		t.Error("OverridesOutput must agree with the adopted flag")
	}

	// A genuinely blank template stays a follower.
	blank := Template{Name: "空白"}
	blank.Normalize()
	if blank.OutputOverride {
		t.Error("a template with no output values must stay a follower")
	}
}

// The three sections are inherited whole, not field by field. A nil section is
// the "follow the global" marker and must stay distinguishable from an empty one.
func TestEffectiveInheritsSectionsWhole(t *testing.T) {
	g := DefaultGlobalTemplate()
	g.Perf.Concurrency = 4
	g.Filter.MinSizeMB = 300
	g.Problems.ErrorAction = ActionMove

	// A follower: all three nil.
	follower := Template{Name: "跟随全局"}.Effective(g)
	if follower.Perf != g.Perf || follower.Filter != g.Filter || follower.Problems != g.Problems {
		t.Error("expected the sections to be taken from the global template")
	}
	if follower.Perf.Concurrency != 4 || follower.Filter.MinSizeMB != 300 {
		t.Errorf("inherited values wrong: %+v %+v", follower.Perf, follower.Filter)
	}

	// An overrider: only Perf is replaced, the other two follow.
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
	if tpl.Perf != nil || tpl.Filter != nil || tpl.Problems != nil {
		t.Error("Normalize materialised a section that was meant to follow the global one")
	}
	if tpl.OutMode != "" {
		t.Errorf("Normalize filled an output mode: %q", tpl.OutMode)
	}
}

// ---------------------------------------------------------------------------
// Global template lifecycle
// ---------------------------------------------------------------------------

func TestEnsureGlobalCreatesAndPins(t *testing.T) {
	list, changed := EnsureGlobal(nil, nil)
	if !changed || len(list) != 1 {
		t.Fatalf("expected a single created global template, got %d (changed=%v)", len(list), changed)
	}
	if !list[0].Global || list[0].ID != GlobalTemplateID {
		t.Errorf("created template is not the global one: %+v", list[0])
	}

	// A list that already has it somewhere else gets it moved to the front.
	list = []Template{{ID: "a", Name: "A"}, {ID: GlobalTemplateID, Name: GlobalTemplateName, Global: true}}
	list, changed = EnsureGlobal(list, nil)
	if !changed || list[0].ID != GlobalTemplateID {
		t.Fatalf("global template was not pinned first: %+v", list)
	}
	// Idempotent: a second pass must report no change so the file is not rewritten.
	if _, changed := EnsureGlobal(list, nil); changed {
		t.Error("EnsureGlobal is not idempotent")
	}
}

// A settings file that predates the global template must land its old values in
// the new global template rather than being dropped.
func TestNewGlobalFromLegacyMovesOutputRules(t *testing.T) {
	s := DefaultSettings()
	s.LegacyOutputDirMode = OutputMirror
	s.LegacyOutputDir = filepath.Join("D:", "Media", "out")
	s.LegacyNamePattern = "{name}.{ext}"
	s.LegacyConflict = ConflictSkip
	s.LegacyConcurrency = 3
	s.LegacyRetryCount = 2
	s.LegacyFilters = &LegacyFilterRules{
		MinSizeMB: 300, Action: ActionMove,
		TargetDir: filepath.Join("D:", "small"),
	}
	s.LegacyOnErrorDir = filepath.Join("D:", "bad")

	g, moved := NewGlobalFromLegacy(s)
	if !moved {
		t.Fatal("expected the legacy values to be reported as moved")
	}
	if g.OutMode != OutputMirror || g.OutDir != s.LegacyOutputDir ||
		g.OutPattern != "{name}.{ext}" || g.OutConflict != ConflictSkip {
		t.Errorf("output rules not migrated: %+v", g)
	}
	if g.Perf.Concurrency != 3 || g.Perf.RetryCount != 2 {
		t.Errorf("performance not migrated: %+v", g.Perf)
	}
	if g.Filter.MinSizeMB != 300 || g.Filter.Action != ActionMove {
		t.Errorf("filter not migrated: %+v", g.Filter)
	}
	// The old implementation always mirrored the tree for problem files, so the
	// migration must not quietly change where those files land.
	if g.Problems.ErrorDest.Mode != OutputMirror || g.Problems.ErrorDest.Dir != s.LegacyOnErrorDir {
		t.Errorf("problem destination not migrated: %+v", g.Problems)
	}

	// After clearing, nothing is left to migrate and the fields disappear from
	// the JSON entirely.
	s.ClearLegacy()
	if s.HasLegacy() {
		t.Error("ClearLegacy left something behind")
	}
	if _, moved := NewGlobalFromLegacy(s); moved {
		t.Error("a cleared settings file must not report a migration")
	}
	raw, _ := json.Marshal(s)
	for _, key := range []string{`"concurrency"`, `"outputDirMode"`, `"filters"`, `"onErrorDir"`} {
		if strings.Contains(string(raw), key) {
			t.Errorf("%s still present in settings.json: %s", key, raw)
		}
	}
}

// The old settings.json keys must still parse, otherwise the migration could
// never run: the fields are flat with omitempty precisely so they can be read
// once and then disappear.
func TestLegacyFieldsParseFromOldJSON(t *testing.T) {
	raw := []byte(`{
		"outputDirMode":"custom","outputDir":"D:/Media","outputSuffix":"_o",
		"namePattern":"{name}","conflict":"skip",
		"concurrency":4,"logLevel":"info","retryCount":1,"deleteOnFail":true,
		"filters":{"minSizeMB":300,"maxSizeMB":9000,"action":"move","targetDir":"D:/small","renamePattern":"x_{name}.{ext}"},
		"onErrorAction":"copy","onErrorDir":"D:/bad","onWarningAction":"mark"
	}`)
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("old settings.json no longer parses: %v", err)
	}
	if !s.HasLegacy() {
		t.Fatal("HasLegacy did not notice the old keys")
	}
	g, moved := NewGlobalFromLegacy(s)
	if !moved {
		t.Fatal("expected a migration")
	}
	if g.OutDir != "D:/Media" || g.Perf.Concurrency != 4 || !g.Perf.DeleteOnFail {
		t.Errorf("migration lost values: %+v %+v", g, g.Perf)
	}
	if g.Filter.MaxSizeMB != 9000 || g.Filter.RenamePattern != "x_{name}.{ext}" {
		t.Errorf("filter migration incomplete: %+v", g.Filter)
	}
	if g.Problems.WarningAction != ActionMark {
		t.Errorf("warning action lost: %+v", g.Problems)
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

	n := NewFromGlobal(g)
	if n.Perf == g.Perf || n.Filter == g.Filter || n.Problems == g.Problems {
		t.Fatal("sections are shared with the global template")
	}
	n.Perf.Concurrency = 1
	n.Filter.IncludeExts[0] = "mkv"
	if g.Perf.Concurrency != 8 || g.Filter.IncludeExts[0] != "mp4" {
		t.Error("editing the new template reached back into the global one")
	}
	if n.OutMode != "" || n.OutputOverride {
		// The output section now has its own switch, and a new template starts
		// switched off. Copying the values in would mean a fresh preset is
		// overriding rules the moment it is created.
		t.Errorf("a new template should start following the output section: %+v", n)
	}
	// ...and following must actually resolve to the global values.
	eff := n.Effective(g)
	if eff.OutMode != g.OutMode || eff.OutSuffix != g.OutSuffix || eff.OutPattern != g.OutPattern {
		t.Errorf("following did not resolve to the global output rules: %+v", eff)
	}
}

// ---------------------------------------------------------------------------
// DestRule
// ---------------------------------------------------------------------------

func TestDestRuleInherit(t *testing.T) {
	fb := DestRule{Mode: OutputMirror, Dir: filepath.Join("D:", "fallback"), Suffix: "_out"}
	got := DestRule{}.Inherit(fb)
	if got != fb {
		t.Errorf("blank rule did not inherit: %+v", got)
	}
	partial := DestRule{Suffix: "_x"}.Inherit(fb)
	if partial.Mode != OutputMirror || partial.Dir != fb.Dir || partial.Suffix != "_x" {
		t.Errorf("partial inherit wrong: %+v", partial)
	}
}

// custom / mirror without a directory cannot be used; same / sibling always can.
// Silently treating the first two as "next to the source" would write results
// somewhere the user never asked for.
func TestDestRuleUsable(t *testing.T) {
	cases := []struct {
		rule DestRule
		want bool
	}{
		{DestRule{Mode: OutputSame}, true},
		{DestRule{Mode: OutputSibling}, true},
		{DestRule{Mode: OutputCustom, Dir: "D:/x"}, true},
		{DestRule{Mode: OutputCustom}, false},
		{DestRule{Mode: OutputMirror, Dir: "D:/x"}, true},
		{DestRule{Mode: OutputMirror}, false},
		{DestRule{Mode: ""}, true},
	}
	for _, tc := range cases {
		if got := tc.rule.Usable(); got != tc.want {
			t.Errorf("%+v usable = %v, want %v", tc.rule, got, tc.want)
		}
	}
	if err := (DestRule{Mode: OutputCustom}).Validate("筛选转移"); err == nil {
		t.Error("expected a validation error for a custom rule with no directory")
	}
	if err := (DestRule{Mode: "nonsense"}).Validate("筛选转移"); err == nil {
		t.Error("expected a validation error for an unknown mode")
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

// HandlesExcluded is what decides whether an excluded file gets relocated, so it
// must be false whenever the action or the destination is unusable.
func TestFilterSpecHandlesExcluded(t *testing.T) {
	cases := []struct {
		name string
		spec FilterSpec
		want bool
	}{
		{"保持原处", FilterSpec{Action: ActionKeep}, false},
		{"移动但没目录", FilterSpec{Action: ActionMove, Dest: DestRule{Mode: OutputCustom}}, false},
		{"移动到指定目录", FilterSpec{Action: ActionMove, Dest: DestRule{Mode: OutputCustom, Dir: "D:/x"}}, true},
		{"复制到镜像目录", FilterSpec{Action: ActionCopy, Dest: DestRule{Mode: OutputMirror, Dir: "D:/x"}}, true},
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
		ErrorAction: ActionMove, ErrorDest: DestRule{Mode: OutputCustom, Dir: "D:/err"},
		WarningAction: ActionMark, WarningDest: DestRule{Mode: OutputMirror, Dir: "D:/warn"},
	}
	if p.Handles(StatusWarning) {
		t.Error("mark must not relocate a warning file")
	}
	if !p.Handles("error") {
		t.Error("move should relocate an error file")
	}
	if p.Action(StatusWarning) != ActionMark || p.Dest(StatusWarning).Dir != "D:/warn" {
		t.Errorf("warning routing wrong: %q %+v", p.Action(StatusWarning), p.Dest(StatusWarning))
	}
	if p.Action("error") != ActionMove || p.Dest("error").Dir != "D:/err" {
		t.Errorf("error routing wrong: %q %+v", p.Action("error"), p.Dest("error"))
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
