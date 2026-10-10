package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRuleFromLegacy(t *testing.T) {
	cases := []struct {
		mode, dir, suffix string
		want              DirSpec
		ok                bool
	}{
		{"sibling", "", "_out", DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}, true},
		// 后缀留空时旧代码用 _out 兜底，翻译要跟上，否则目录会少一截。
		{"sibling", "", "", DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}, true},
		{"same", "", "_out", DirSpec{Mode: OutputSame}, true},
		{"custom", "D:/video", "", DirSpec{Mode: OutputCustom, Dir: "D:/video"}, true},
		// 旧代码把"指定目录"和"指定目录 + 保留子目录"做成两个模式，现在是一个模式
		// 加一个开关，所以两个都要落到 custom 上。
		{"mirror", "D:/video", "", DirSpec{Mode: OutputCustom, Dir: "D:/video", KeepTree: true}, true},
		// 没写过模式 = 没设置过。翻译出一个没人选过的覆盖，比不翻译更糟。
		{"", "D:/video", "", DirSpec{}, false},
		{"wat", "", "", DirSpec{}, false},
	}
	for _, c := range cases {
		got, ok := ruleFromLegacy(c.mode, c.dir, c.suffix)
		if ok != c.ok || got != c.want {
			t.Errorf("ruleFromLegacy(%q,%q,%q) = %+v,%v want %+v,%v",
				c.mode, c.dir, c.suffix, got, ok, c.want, c.ok)
		}
	}
}

// 第 34 ~ 38 批那条表达式。装得下的按原样翻译，装不下的往"同级目录"退 —
// 产物落在源文件夹外面是这里唯一不能让的事。
func TestSpecFromExpr(t *testing.T) {
	cases := []struct {
		expr     string
		keepTree bool
		want     DirSpec
	}{
		{"", false, DirSpec{Mode: OutputSame}},
		// 没有变量的就是一条普通目录路径，什么都不追加。
		{"D:/video", false, DirSpec{Mode: OutputCustom, Dir: "D:/video"}},
		{"D:/video", true, DirSpec{Mode: OutputCustom, Dir: "D:/video", KeepTree: true}},
		// "{rootDir}_out" 是那一版最常见的写法，正好就是现在这个模式加一个后缀。
		{"{rootDir}_out", true, DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}},
		{"{rootDir}_done", false, DirSpec{Mode: OutputSibling, Suffix: "_done"}},
		{"{rootDir}", true, DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}},
		// 带变量的其它写法没有对应的模式，退到默认的同级目录。
		{"{rootDir:-1}\\123_{rootName}", true, DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}},
		{"{dirPath}/ffmpeg", false, DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}},
	}
	for _, c := range cases {
		if got := specFromExpr(c.expr, c.keepTree); got != c.want {
			t.Errorf("specFromExpr(%q,%v) = %+v, want %+v", c.expr, c.keepTree, got, c.want)
		}
	}
}

// 翻译过来的规则必须是能跑的：老文件里 custom/mirror 但目录为空的那种，落到新写法
// 上就是"写在源文件旁边"，而不是一条 MkdirAll("") 的错误。
func TestSpecFromLegacyResolves(t *testing.T) {
	src := filepath.Join(dirRoot, "mmd", "a.mp4")
	for _, got := range []DirSpec{
		ruleFromLegacyMust(t, "custom", "", ""),
		ruleFromLegacyMust(t, "mirror", "  ", ""),
	} {
		if dir := ResolveDestDir(DestRequest{Spec: got, SrcPath: src, SrcRoot: dirRoot}); dir != dirRoot+`\mmd` {
			t.Errorf("%+v -> %q, want %q", got, dir, dirRoot+`\mmd`)
		}
	}
}

func ruleFromLegacyMust(t *testing.T, mode, dir, suffix string) DirSpec {
	t.Helper()
	spec, ok := ruleFromLegacy(mode, dir, suffix)
	if !ok {
		t.Fatalf("ruleFromLegacy(%q,%q,%q) reported unset", mode, dir, suffix)
	}
	return spec
}

// The line the real templates.json carried: outMode "sibling" + outSuffix "_out"
// on the global template. Read as "not set" it resolves to "next to the source
// file" -- every existing user's outputs would move into the folder they scan.
func TestLoadTemplatesTranslatesLegacyOutputRule(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	legacy := []map[string]any{
		{"id": GlobalTemplateID, "name": "全局模板", "global": true, "outputOverride": true,
			"outMode": "sibling", "outDir": "", "outSuffix": "_out", "outPattern": "{name}",
			"problems": map[string]any{
				"errorAction":   "keep",
				"errorDest":     map[string]any{"mode": "custom", "dir": "", "suffix": ""},
				"warningAction": "mark",
				"warningDest":   map[string]any{"mode": "mirror", "dir": "", "suffix": ""},
			}},
		{"id": "a1", "name": "跟随全局", "outPattern": ""},
		{"id": "a2", "name": "自己选了目录", "outputOverride": true, "outMode": "custom", "outDir": "D:/video"},
		// 接管这个开关是后加的，所以老文件里可能根本没有它。少了这一次补写，
		// 刚翻译出来的规则会被 Effective 当成"跟随全局"直接丢掉。
		{"id": "a3", "name": "老文件没这个键", "outMode": "custom", "outDir": "D:/other"},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TemplatesPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}

	list := LoadTemplates()
	if len(list) != 4 {
		t.Fatalf("got %d templates, want 4", len(list))
	}
	g := GlobalOrDefault(list)
	if want := (DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}); g.OutDirSpec != want {
		t.Errorf("global OutDirSpec = %+v, want %+v", g.OutDirSpec, want)
	}
	// 旧代码在"指定目录但目录为空"时报错；翻译过来的是一个能跑的结果。
	if p := g.Problems; p == nil || p.ErrorDir != (DirSpec{Mode: OutputCustom}) ||
		p.WarningDir != (DirSpec{Mode: OutputCustom, KeepTree: true}) {
		t.Errorf("problem dirs = %+v / %+v", p.ErrorDir, p.WarningDir)
	}
	// The templates themselves: one follows the global, the others keep the
	// directory they chose.
	if eff := list[1].Effective(g); eff.OutDirSpec.Mode != OutputSibling || eff.OutDirSpec.Suffix != "_out" {
		t.Errorf("following template resolved to %+v", eff.OutDirSpec)
	}
	if eff := list[2].Effective(g); eff.OutDirSpec != (DirSpec{Mode: OutputCustom, Dir: "D:/video"}) {
		t.Errorf("overriding template resolved to %+v", eff.OutDirSpec)
	}
	if eff := list[3].Effective(g); eff.OutDirSpec != (DirSpec{Mode: OutputCustom, Dir: "D:/other"}) {
		t.Errorf("template without the override key resolved to %+v", eff.OutDirSpec)
	}
}

// 第 34 ~ 38 批那条表达式的文件。path 键在，outMode 键不在 —— 走的是另一条翻译。
func TestLoadTemplatesTranslatesLegacyDirectoryExpression(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	legacy := []map[string]any{
		{"id": GlobalTemplateID, "name": "全局模板", "global": true, "outputOverride": true,
			"outDirSpec": map[string]any{"path": "{rootDir}_out", "keepTree": true}},
		{"id": "b1", "name": "表达式", "outputOverride": true,
			"outDirSpec": map[string]any{"path": `{rootDir:-1}\123_{rootName}`, "keepTree": true}},
		{"id": "b2", "name": "普通路径", "outputOverride": true,
			"outDirSpec": map[string]any{"path": "D:/video", "keepTree": false}},
		// 这一版的文件里不该再有 outMode；留着也没关系，翻译以 path 为准。
		{"id": "b3", "name": "留空", "outputOverride": true,
			"outMode": "custom", "outDir": "D:/ignored",
			"outDirSpec": map[string]any{"path": "", "keepTree": false}},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TemplatesPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}

	list := LoadTemplates()
	if len(list) != 4 {
		t.Fatalf("got %d templates, want 4", len(list))
	}
	if want := (DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}); list[0].OutDirSpec != want {
		t.Errorf("global = %+v, want %+v", list[0].OutDirSpec, want)
	}
	if want := (DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true}); list[1].OutDirSpec != want {
		t.Errorf("expression = %+v, want %+v", list[1].OutDirSpec, want)
	}
	if want := (DirSpec{Mode: OutputCustom, Dir: "D:/video"}); list[2].OutDirSpec != want {
		t.Errorf("literal = %+v, want %+v", list[2].OutDirSpec, want)
	}
	if want := (DirSpec{Mode: OutputSame}); list[3].OutDirSpec != want {
		t.Errorf("blank = %+v, want %+v", list[3].OutDirSpec, want)
	}
}

// 新写的文件不该被老键改回去：translate 只在目标字段还空着时才动手，而这一版
// 每一段至少都写下了 mode 或 keepTree。
func TestAdoptLegacyLeavesCurrentFilesAlone(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	list := []Template{{
		ID: GlobalTemplateID, Name: GlobalTemplateName, Global: true, OutputOverride: true,
		OutDirSpec: DirSpec{Mode: OutputCustom, Dir: "D:/media", KeepTree: true},
	}}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TemplatesPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got := GlobalOrDefault(LoadTemplates())
	if want := (DirSpec{Mode: OutputCustom, Dir: "D:/media", KeepTree: true}); got.OutDirSpec != want {
		t.Errorf("a current file was rewritten: %+v", got.OutDirSpec)
	}
}
