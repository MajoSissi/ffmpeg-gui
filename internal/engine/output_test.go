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

// siblingRule is the shipped default: results land in a folder next to the one
// that was added, suffixed, with the source's sub-tree rebuilt underneath.
//
//	D:\video\mmd\a.mp4 -> D:\video\mmd_out\a.mp4
//
// The two directory rules these tests keep needing: 「同级目录 + 后缀」 next to the
// added folder, and its _done cousin for the relocation sections.
func siblingOut(keepTree bool) store.DirSpec {
	return store.DirSpec{Mode: store.OutputSibling, Suffix: "_out", KeepTree: keepTree}
}

func siblingDone(keepTree bool) store.DirSpec {
	return store.DirSpec{Mode: store.OutputSibling, Suffix: "_done", KeepTree: keepTree}
}

// globalWith builds a global template carrying the given output rule. The engine
// no longer reads output rules from settings -- they all come from here.
// The 「已处理过的文件」 policy is a separate section and irrelevant here:
// ResolveOutput names the path and leaves an occupied one alone.
func globalWith(spec store.DirSpec, pattern string) store.Template {
	g := store.DefaultGlobalTemplate()
	g.OutDirSpec = spec
	g.OutPattern = pattern
	return g
}

// withSiblingRule gives tpl an explicit sibling-directory rule and merges it with
// global.
//
// The tests below are about naming and extensions, so they need the directory they
// assert on to be the template's own. They used to get it by leaving the output
// section blank and inheriting it, which stopped working the moment blank output
// fields became "the plain default" instead of "whatever the global template
// says" -- a blank expression now means next to the source file, and every
// "root_out" expectation would be measuring the wrong thing.
func withSiblingRule(tpl store.Template, global store.Template) store.Template {
	tpl.OutputOverride = true
	tpl.OutDirSpec = siblingOut(true)
	return tpl.Effective(global)
}

// 夹具写**字面量**而不是 filepath.Join("D:", "video", "mmd")：后者在 Windows 上是
// 盘相对路径 D:video\mmd，看着像绝对路径，而 filepath.Dir 对它只给出 D:video ——
// 同级目录算的正是 dirname，基准错了整张表都在测别的东西。

// TestResolveOutputSiblingRule mirrors the workflow the default rule exists for:
// process a whole folder tree and land every result in ONE folder next to the tree.
//
// The names are left alone, so D:\video\mmd\a.mp4 becomes D:\video\mmd_out\a.mp4.
// Decorating the names too (a_out.mp4) would put a second "_out" on every result
// for no benefit.
//
// 「同级目录」量的是**添加的那个目录**，所以 mmd\sub\b.mp4 落在
// D:\video\mmd_out\sub\b.mp4 —— 整棵树只有一个产物目录，而不是每个子目录旁边各出一个。
func TestResolveOutputSiblingRule(t *testing.T) {
	root := `D:\video\mmd`
	global := globalWith(siblingOut(true), "")

	sibling := filepath.Join(`D:\video`, "mmd_out")
	for _, tc := range []struct {
		name string
		rel  string // file path relative to root
		want string // expected path, written out in full
	}{
		{"根目录下的文件", "a.mp4", filepath.Join(sibling, "a.mp4")},
		{"一层子目录", filepath.Join("sub", "b.mp4"), filepath.Join(sibling, "sub", "b.mp4")},
		{"多层子目录", filepath.Join("x", "y", "c.mp4"), filepath.Join(sibling, "x", "y", "c.mp4")},
	} {
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
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// 「同级顶层目录」（第四档）量的是**文件所在的那一层**：每个子目录旁边各出一个，
// 所以同一棵树会得到 mmd_out / mmd\sub_out / mmd\x\y_out 三个目录，而不是一个。
// 这两条必须一起钉住：它们只差一个锚，而"看起来差不多"正是上一次把锚改错、
// 一路改回去还没人发现的原因。
func TestResolveOutputSiblingTopRule(t *testing.T) {
	root := `D:\video\mmd`
	global := globalWith(store.DirSpec{
		Mode: store.OutputSiblingTop, Suffix: "_out", KeepTree: true,
	}, "")

	for _, tc := range []struct {
		name string
		rel  string
		want string
	}{
		{"根目录下的文件", "a.mp4", filepath.Join(`D:\video`, "mmd_out", "a.mp4")},
		{"一层子目录", filepath.Join("sub", "b.mp4"), filepath.Join(root, "sub_out", "b.mp4")},
		{"多层子目录", filepath.Join("x", "y", "c.mp4"), filepath.Join(root, "x", "y_out", "c.mp4")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(root, tc.rel)
			eff := store.Template{}.Effective(global)
			got, err := ResolveOutput(OutputRequest{
				Info: mkInfo(src), Tpl: eff, SrcRoot: root,
			})
			if err != nil {
				t.Fatalf("ResolveOutput: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The shipped default is a rule like any other, and it has to keep resolving to
// the folder it now means: 同级目录 measures from the ADDED folder, so every
// result sits under that one folder -- never inside the tree it came from, which is
// the part a default must not get wrong.
func TestDefaultGlobalKeepsTheSiblingRule(t *testing.T) {
	root := `D:\video\mmd`
	src := filepath.Join(root, "sub", "b.mp4")
	eff := store.Template{}.Effective(store.DefaultGlobalTemplate())
	got, err := ResolveOutput(OutputRequest{Info: mkInfo(src), Tpl: eff, SrcRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root+"_out", "sub", "b.mp4"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A template that carries its own directory expression must win over the global
// one. The switch is what makes the template's values count at all.
func TestResolveOutputTemplateOverride(t *testing.T) {
	root := `D:\video\mmd`
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(store.DirSpec{}, "{name}")

	eff := store.Template{
		OutDirSpec:     siblingDone(true),
		OutputOverride: true,
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
	root := `D:\video\mmd`
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(siblingOut(true), "{name}")

	eff := store.Template{
		OutDirSpec:     store.DirSpec{Mode: store.OutputCustom, Dir: filepath.Join("D:", "stale")},
		OutputOverride: false,
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

// A template that turns the output switch on and leaves both fields blank writes
// next to the source file under the source's own name.
//
// Blank is the plain default here, not "whatever the global template says": the
// switch is the only way to say "follow", so a blank field inside an overridden
// section must not follow as well.
func TestResolveOutputBlankRuleStaysLocal(t *testing.T) {
	root := `D:\video\mmd`
	src := filepath.Join(root, "sub", "b.mp4")

	global := globalWith(siblingOut(true), "{name}_glob")

	// Blank directory and blank pattern: the result would be the input itself, so
	// the never-overwrite-the-source guard is what names it.
	eff := store.Template{OutputOverride: true}.Effective(global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(src), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatalf("ResolveOutput: %v", err)
	}
	if want := filepath.Join(root, "sub", "b_out.mp4"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A pattern of its own still lands in the source directory -- the global
	// sibling folder is not consulted at all.
	eff2 := store.Template{OutputOverride: true, OutPattern: "{name}_new"}.Effective(global)
	got2, err := ResolveOutput(OutputRequest{Info: mkInfo(src), Tpl: eff2, SrcRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "sub", "b_new.mp4"); got2 != want {
		t.Errorf("got %q, want %q", got2, want)
	}
}

// The extension comes from 「输出格式」, not from the naming template. {ext} used to be
// the way to spell it out, which made the output depend on two settings at once; a
// plain {name} now yields the container's extension, so an MKV source named .mp4 is
// the expected result rather than a bug.
func TestResolveOutputExtFollowsContainer(t *testing.T) {
	root := `D:\video\mmd`
	src := filepath.Join(root, "a.mkv")

	global := globalWith(siblingOut(true), "{name}")

	eff := withSiblingRule(store.Template{Container: "mp4"}, global)
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
	legacy := withSiblingRule(store.Template{Container: "mp4", OutPattern: "{name}.{ext}"}, global)
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

// Every stage that writes a file asks the same question of the same function, so
// one table covers the main output, the filter transfer and the problem files.
func TestResolveDestDirSharedByEveryStage(t *testing.T) {
	root := `D:\video\mmd`
	src := filepath.Join(root, "sub", "b.mp4")
	flat := filepath.Join("D:", "Media", "flat")

	cases := []struct {
		name string
		spec store.DirSpec
		want string
	}{
		{"留空 = 源目录", store.DirSpec{}, filepath.Join(root, "sub")},
		{"自定义目录原样使用", store.DirSpec{Mode: store.OutputCustom, Dir: flat}, flat},
		{"自定义目录 + 保留结构", store.DirSpec{Mode: store.OutputCustom, Dir: flat, KeepTree: true}, filepath.Join(flat, "sub")},
		// 同级目录量的是**添加的那个目录**，所以文件在 sub 里时落点是
		// mmd_out/sub —— 子目录靠「保留目录结构」接回去，而顶层只有 mmd_out 一个。
		{"同级目录 + 保留结构", siblingOut(true), filepath.Join(root+"_out", "sub")},
		{"同级目录不保留结构", siblingOut(false), filepath.Join(root + "_out")},
		// 同级顶层目录量的是**文件所在的目录**：sub 的同级是 mmd，所以落点
		// mmd/sub_out —— 一棵子目录不一的树会得到 N 个产物目录。
		{"同级顶层目录", store.DirSpec{Mode: store.OutputSiblingTop, Suffix: "_out"},
			filepath.Join(root, "sub_out")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := store.ResolveDestDir(store.DestRequest{
				Spec: tc.spec, SrcPath: src, SrcRoot: root,
			})
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
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
		Dirs:    store.DirSpec{Mode: store.OutputCustom, Dir: filepath.Join(root, "failed")},
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
		Dirs:    store.DirSpec{Mode: store.OutputCustom, Dir: filepath.Join(root, "failed")},
		Pattern: "{name}_bad_{dir}.{ext}",
	})
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if filepath.Base(got) != "clip2_bad_"+filepath.Base(root)+".mkv" {
		t.Errorf("got %q -- {ext} must be the source extension", filepath.Base(got))
	}
}

// Moving a file to where it already is reports "nothing happened" -- an empty
// path -- instead of a destination.
//
// The uniqueness check would otherwise read the file as a clash with itself and
// hand back a_1.mp4: nothing would have moved, but the name would have changed,
// and a batch whose rule says "留在原处" would look like it had relocated every
// file in it.
func TestRelocateToSamePlaceIsANoop(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.mp4")
	writeFile(t, src, "payload")

	dest, err := Relocate(MoveRequest{Src: src, SrcRoot: dir})
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if dest != "" {
		t.Errorf("dest = %q, want empty (nothing to do)", dest)
	}
	if !existsAt(t, src) {
		t.Error("the source disappeared")
	}
	if existsAt(t, filepath.Join(dir, "a_1.mp4")) {
		t.Error("the file was renamed to a_1.mp4 instead of being left alone")
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

// The index width has to mean the same thing in both patterns. {index} is a plain
// number now, so a template that still wants 007 writes {index:3} -- and the
// directory expression resolves it with the same code (store.FormatIndex), which
// is what keeps "输出目录" and "输出文件名称" from disagreeing.
func TestIndexWidthInNamingPatterns(t *testing.T) {
	n := Naming{Name: "clip", Ext: "mp4", Index: 7}
	for _, tc := range []struct{ pat, want string }{
		{"{index}", "7"},
		{"{index:0}", "7"},
		{"{index:1}", "7"},
		{"{index:2}", "07"},
		{"{index:3}", "007"},
		{"{idx:3}", "007"},
	} {
		if got := ExpandOutputPattern(tc.pat, n); got != tc.want {
			t.Errorf("ExpandOutputPattern(%q) = %q, want %q", tc.pat, got, tc.want)
		}
		if got := ExpandPattern(tc.pat, n); got != tc.want {
			t.Errorf("ExpandPattern(%q) = %q, want %q", tc.pat, got, tc.want)
		}
	}
	// {ext} 在这两个 pattern 里仍然不是一回事：文件名那份把它去掉（扩展名由
	// 「输出格式」给），搬迁那份留着。
	if got := ExpandOutputPattern("{idx:3}.{ext}", n); got != "007." {
		t.Errorf("ExpandOutputPattern({idx:3}.{ext}) = %q, want %q", got, "007.")
	}
	if got := ExpandPattern("{idx:3}.{ext}", n); got != "007.mp4" {
		t.Errorf("ExpandPattern({idx:3}.{ext}) = %q, want %q", got, "007.mp4")
	}
	// No index in this batch stays empty, and no amount of padding turns that into
	// a literal "000" in a file name.
	if got := ExpandOutputPattern("a{index:3}b", Naming{Name: "clip"}); got != "ab" {
		t.Errorf("no index -> %q, want %q", got, "ab")
	}
	// 名字类 token 不收参数：{name:2} 没法回答"补到第几位"，留着花括号至少能看见。
	if got := ExpandOutputPattern("{name:2}", n); got != "{name:2}" {
		t.Errorf("{name:2} -> %q", got)
	}
}

// A dot in the middle of a name is not an extension. Reported case: a source named
// "qqq.123.mp4" produced the output "qqq.123" -- no extension at all.
//
// The cause is that the stem of "qqq.123.mp4" is "qqq.123", and filepath.Ext of
// that is ".123". A guard written as `Ext(name) == ""` therefore concluded the name
// was complete and skipped appending the container's extension. ffmpeg then had no
// extension to infer a muxer from, so the result was unplayable under its own name.
func TestResolveOutputKeepsExtWhenStemHasDots(t *testing.T) {
	root := `E:\BiliBili`
	global := globalWith(siblingOut(true), "{name}")

	for _, tc := range []struct{ src, want string }{
		{"qqq.123.mp4", "qqq.123.mp4"},
		{"v1.2.3.final.mp4", "v1.2.3.final.mp4"},
		{"[1080p].BDRip.mp4", "[1080p].BDRip.mp4"},
		{"no-dots.mp4", "no-dots.mp4"},
	} {
		eff := withSiblingRule(store.Template{Container: "mp4"}, global)
		got, err := ResolveOutput(OutputRequest{
			Info: mkInfo(filepath.Join(root, tc.src)), Tpl: eff, SrcRoot: root,
		})
		if err != nil {
			t.Fatalf("%s: ResolveOutput: %v", tc.src, err)
		}
		// The suffix lands on the top folder, so the file keeps its name.
		want := filepath.Join(root+"_out", tc.want)
		if got != want {
			t.Errorf("%s -> %q, want %q", tc.src, got, want)
		}
	}
}

// The extension still follows the container, and a pattern that already spells it
// out is not given a second one.
func TestResolveOutputDottedStemFollowsContainer(t *testing.T) {
	root := `E:\BiliBili`
	global := globalWith(siblingOut(true), "{name}")

	// Source is mp4, container asks for mkv: the dotted stem must survive the swap.
	eff := withSiblingRule(store.Template{Container: "mkv"}, global)
	got, err := ResolveOutput(OutputRequest{
		Info: mkInfo(filepath.Join(root, "qqq.123.mp4")), Tpl: eff, SrcRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root+"_out", "qqq.123.mkv"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A pattern that already ends with the container extension must not double it.
	explicit := withSiblingRule(
		store.Template{Container: "mp4", OutPattern: "{name}.mp4"}, global)
	got2, err := ResolveOutput(OutputRequest{
		Info: mkInfo(filepath.Join(root, "qqq.123.mp4")), Tpl: explicit, SrcRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root+"_out", "qqq.123.mp4"); got2 != want {
		t.Errorf("explicit extension gave %q, want %q", got2, want)
	}
}

// EnsureExt is the rule in one place: compare against the extension we intend to
// write, never against "is there a dot".
func TestEnsureExt(t *testing.T) {
	for _, tc := range []struct{ name, ext, want string }{
		{"clip", "mp4", "clip.mp4"},
		{"clip.mp4", "mp4", "clip.mp4"},
		{"clip.MP4", "mp4", "clip.MP4"}, // already right, case-insensitively
		{"qqq.123", "mp4", "qqq.123.mp4"},
		{"v1.2.3", "mkv", "v1.2.3.mkv"},
		{"clip.", "mp4", "clip..mp4"}, // trailing dot handled by the caller
		{"clip", "", "clip"},          // no container: leave it alone
	} {
		if got := EnsureExt(tc.name, tc.ext); got != tc.want {
			t.Errorf("EnsureExt(%q, %q) = %q, want %q", tc.name, tc.ext, got, tc.want)
		}
	}
}

// Relocating a file has the same trap: the moved file keeps its own extension, and
// "qqq.123" is not a file that already has one.
func TestRelocateKeepsExtWhenStemHasDots(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "qqq.123.mp4")
	writeFile(t, src, "payload")

	dest, err := Relocate(MoveRequest{
		Src:     src,
		SrcRoot: dir,
		Dirs:    store.DirSpec{Mode: store.OutputCustom, Dir: filepath.Join(dir, "moved")},
		// {name} only: the extension has to be re-attached by the mover.
		Pattern: "{name}",
	})
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if want := filepath.Join(dir, "moved", "qqq.123.mp4"); dest != want {
		t.Errorf("got %q, want %q", dest, want)
	}
	if !existsAt(t, dest) {
		t.Error("the file did not actually land at the reported path")
	}
}
