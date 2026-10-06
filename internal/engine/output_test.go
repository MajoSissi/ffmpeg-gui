package engine

import (
	"os"
	"path/filepath"
	"testing"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

func mkInfo(path string) *media.Info {
	return &media.Info{Path: path, FileName: filepath.Base(path), Ext: "mp4"}
}

// globalWith builds a global template carrying the given output rules. The
// engine no longer reads output rules from settings -- they all come from here.
// The 「已处理过的源文件」 policy is a separate section and irrelevant here:
// ResolveOutput names the path and leaves an occupied one alone.
func globalWith(mode, suffix, pattern string) store.Template {
	g := store.DefaultGlobalTemplate()
	g.OutMode = mode
	g.OutSuffix = suffix
	g.OutPattern = pattern
	return g
}

// TestResolveOutputSibling mirrors the workflow the mode exists for: process a
// whole folder tree and land the results in a sibling folder, sub-tree intact.
//
// The suffix goes on the top folder only and the file names are left alone, so
// D:\video\mmd\a.mp4 becomes D:\video\mmd_out\a.mp4. Decorating the names too
// (a_out.mp4) put a second "_out" on every result for no benefit.
func TestResolveOutputSibling(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	global := globalWith(store.OutputSibling, "_out", "")

	cases := []struct {
		name string
		rel  string // file path relative to root
		want string // expected path relative to root+"_out"
	}{
		{"根目录下的文件", "a.mp4", "a.mp4"},
		{"一层子目录", filepath.Join("sub", "b.mp4"), filepath.Join("sub", "b.mp4")},
		{"多层子目录", filepath.Join("x", "y", "c.mp4"), filepath.Join("x", "y", "c.mp4")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(root, tc.rel)
			// A blank template inherits the global rules verbatim.
			eff := store.Template{}.Effective(global)
			got, err := ResolveOutput(OutputRequest{
				Info: mkInfo(src), Tpl: eff, SrcRoot: root,
			})
			if err != nil {
				t.Fatalf("ResolveOutput: %v", err)
			}
			want := filepath.Join(root+"_out", tc.want)
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// A template that carries its own output rules must win over the global ones,
// and its suffix must be used instead of the global suffix. The switch is what
// makes the template's values count at all.
func TestResolveOutputTemplateOverride(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(store.OutputSame, "", "{name}")

	eff := store.Template{
		OutMode: store.OutputSibling, OutSuffix: "_done", OutputOverride: true,
	}.Effective(global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	want := filepath.Join(root+"_done", "sub", "b.mp4")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A template whose output switch is off must ignore its own values entirely, even
// when they are set. Otherwise the editor says "following the global template"
// while the command quietly uses something else.
func TestResolveOutputFollowerIgnoresOwnValues(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(store.OutputSibling, "_out", "{name}")

	eff := store.Template{
		OutMode: store.OutputSame, OutSuffix: "_stale", OutputOverride: false,
	}.Effective(global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	want := filepath.Join(root+"_out", "sub", "b.mp4")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A template that leaves every output field blank must inherit the global rule
// unchanged -- that is what makes "留空则跟随全局" true.
func TestResolveOutputInheritsGlobal(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(store.OutputSibling, "_out", "{name}")

	eff := store.Template{OutputOverride: true}.Effective(global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	want := filepath.Join(root+"_out", "sub", "b.mp4")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The extension comes from 「输出格式」, not from the naming template. {ext} used to be
// the way to spell it out, which made the output depend on two settings at once; a
// plain {name} now yields the container's extension, so an MKV source named .mp4 is
// the expected result rather than a bug.
func TestResolveOutputExtFollowsContainer(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "a.mkv")

	global := globalWith(store.OutputSibling, "_out", "{name}")

	eff := store.Template{Container: "mp4", OutputOverride: true}.Effective(global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	want := filepath.Join(root+"_out", "a.mp4")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A template written while {ext} still worked must not leave the token in the
	// name or produce a doubled extension.
	legacy := store.Template{
		Container: "mp4", OutputOverride: true,
		OutPattern: "{name}.{ext}",
	}.Effective(global)
	got2, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: legacy, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput (legacy pattern): %v", err)
	}
	if got2 != want {
		t.Errorf("legacy {{name}}.{{ext}} gave %q, want %q", got2, want)
	}
}

// The four destination rules must behave identically no matter which stage asks:
// the main output, the filter transfer and the problem files all go through
// store.ResolveDestDir, so one table covers them.
func TestResolveDestDirAllModes(t *testing.T) {
	root := filepath.Join("D:", "video", "mmd")
	src := filepath.Join(root, "sub", "b.mp4")
	custom := filepath.Join("D:", "Media", "flat")
	mirror := filepath.Join("D:", "Media", "tree")

	cases := []struct {
		mode string
		dir  string
		want string
	}{
		{store.OutputSame, "", filepath.Join(root, "sub")},
		{store.OutputSibling, "", filepath.Join(root+"_out", "sub")},
		{store.OutputCustom, custom, custom},
		{store.OutputMirror, mirror, filepath.Join(mirror, "sub")},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			got, err := store.ResolveDestDir(store.DestRequest{
				Rule:          store.DestRule{Mode: tc.mode, Dir: tc.dir},
				SrcPath:       src,
				SrcRoot:       root,
				DefaultSuffix: store.DefaultOutputSuffix,
			})
			if err != nil {
				t.Fatalf("ResolveDestDir: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// custom / mirror without a directory is a configuration mistake, not a reason
// to silently write next to the source.
func TestResolveDestDirRejectsEmptyDir(t *testing.T) {
	src := filepath.Join("D:", "video", "mmd", "a.mp4")
	for _, mode := range []string{store.OutputCustom, store.OutputMirror} {
		if _, err := store.ResolveDestDir(store.DestRequest{
			Rule: store.DestRule{Mode: mode}, SrcPath: src,
		}); err == nil {
			t.Errorf("%s with empty dir: expected an error", mode)
		}
	}
}

// The global template's suffix is the last-resort default, so a rule that names
// no suffix of its own can never collapse onto the source folder.
func TestSiblingSuffixFallback(t *testing.T) {
	src := filepath.Join("D:", "video", "mmd", "a.mp4")
	got, err := store.ResolveDestDir(store.DestRequest{
		Rule:          store.DestRule{Mode: store.OutputSibling},
		SrcPath:       src,
		SrcRoot:       filepath.Dir(src),
		DefaultSuffix: store.DefaultOutputSuffix,
	})
	if err != nil {
		t.Fatalf("ResolveDestDir: %v", err)
	}
	want := filepath.Dir(src) + store.DefaultOutputSuffix
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Problem files are renamed with the same pattern language as the output section.
// Two things make it different from there and are easy to get wrong: an empty
// pattern must mean "keep the file's own name", and {ext} is the *source*
// extension -- nothing is re-encoded, the original file is what gets moved.
func TestRelocateProblemFilePattern(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "clip.mkv")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No pattern: the original name survives, extension included.
	got, err := Relocate(MoveRequest{
		Src:     src,
		SrcRoot: root,
		Dest:    store.DestRule{Mode: store.OutputCustom, Dir: filepath.Join(root, "failed")},
	})
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if filepath.Base(got) != "clip.mkv" {
		t.Errorf("empty pattern should keep the file name, got %q", filepath.Base(got))
	}

	// A pattern: {ext} is the source extension here, not an output one.
	src2 := filepath.Join(root, "clip2.mkv")
	if err := os.WriteFile(src2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Relocate(MoveRequest{
		Src:     src2,
		SrcRoot: root,
		Dest:    store.DestRule{Mode: store.OutputCustom, Dir: filepath.Join(root, "failed")},
		Pattern: "{name}_bad_{dir}.{ext}",
	})
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if filepath.Base(got) != "clip2_bad_"+filepath.Base(root)+".mkv" {
		t.Errorf("got %q -- {ext} must be the source extension", filepath.Base(got))
	}
}

// ProblemSpec.Pattern picks the right template per status, so an error rule and a
// warning rule can name their files differently.
func TestProblemSpecPatternPerStatus(t *testing.T) {
	p := store.ProblemSpec{ErrorPattern: "err_{name}.{ext}", WarningPattern: "warn_{name}.{ext}"}
	if got := p.Pattern("error"); got != "err_{name}.{ext}" {
		t.Errorf("error pattern = %q", got)
	}
	if got := p.Pattern(store.StatusWarning); got != "warn_{name}.{ext}" {
		t.Errorf("warning pattern = %q", got)
	}
}

// The two token sets must not leak into each other. A naming template is a file
// name, so {ext} is not available there and the container supplies the extension;
// a file being relocated keeps its own, and there {ext} still means something.
func TestExpandOutputPatternDropsExt(t *testing.T) {
	n := Naming{Name: "clip", Ext: "mkv", Template: "t", Dir: "d", Index: 7}
	if got := ExpandOutputPattern("{name}.{ext}", n); got != "clip." {
		t.Errorf("ExpandOutputPattern left the token in: %q", got)
	}
	if got := ExpandPattern("{name}.{ext}", n); got != "clip.mkv" {
		t.Errorf("ExpandPattern = %q, want %q", got, "clip.mkv")
	}
	// The rest of the tokens are shared by both.
	for _, tok := range []string{"{name}", "{template}", "{dir}", "{index}"} {
		if ExpandOutputPattern(tok, n) == tok || ExpandPattern(tok, n) == tok {
			t.Errorf("%s was not expanded", tok)
		}
	}
}
