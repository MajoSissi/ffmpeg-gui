package store

import (
	"encoding/json"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// 老文件里的目录写法
// ---------------------------------------------------------------------------
//
// 目标目录换过两次形状，这里把两种老写法都翻译成现在这一版：
//
//	第 34 批之前   三个扁平字段 outMode/outDir/outSuffix，四段搬迁各有一个
//	                {mode,dir,suffix} 对象（dest / errorDest / warningDest）
//	第 34 ~ 38 批  一条目录表达式 {path,keepTree}，变量直接写在 path 里
//	现在           {mode,dir,prefix,suffix,keepTree} —— 模式 + 字段
//
// **为什么不能像 settings.json 的旧键那样直接忽略**：sibling + _out 是最常见的一种
// 配置，翻译时丢掉它，产物就会从 `<根目录>_out` 悄悄搬进源文件夹 —— 而且下一次扫描
// 同一个目录时，这些产物会被当成输入再吃一遍。翻译一次比让这种事发生便宜得多。
//
// 翻译结果不写回文件：下一次 SaveTemplates 只写新字段，老键自然消失。
// 这个文件可以整体删掉——等到没有人的 templates.json 里还有 outMode 或 path。

// legacyRule is the {mode,dir,suffix} triple of 第 34 批之前.
type legacyRule struct {
	Mode   string `json:"mode"`
	Dir    string `json:"dir"`
	Suffix string `json:"suffix"`
}

// legacySpec is the directory expression of 第 34 ~ 38 批. Path is a pointer on
// purpose: only a key that is really in the file counts as that version, so
// "absent" and "empty" stay two different things.
type legacySpec struct {
	Path     *string `json:"path"`
	KeepTree bool    `json:"keepTree"`
}

// legacyTemplate holds only the keys these translations read. Everything else in
// the file is decoded into Template as usual.
type legacyTemplate struct {
	OutMode   string      `json:"outMode"`
	OutDir    string      `json:"outDir"`
	OutSuffix string      `json:"outSuffix"`
	OutSpec   *legacySpec `json:"outDirSpec"`

	Existing *struct {
		Dest legacyRule  `json:"dest"`
		Dir  *legacySpec `json:"dir"`
	} `json:"existing"`
	Filter *struct {
		Dest legacyRule  `json:"dest"`
		Dir  *legacySpec `json:"dir"`
	} `json:"filter"`
	Problems *struct {
		ErrorDest   legacyRule  `json:"errorDest"`
		WarningDest legacyRule  `json:"warningDest"`
		ErrorDir    *legacySpec `json:"errorDir"`
		WarningDir  *legacySpec `json:"warningDir"`
	} `json:"problems"`
}

// ruleFromLegacy translates one 第 34 批之前 的三元组. It reports false when the mode
// says nothing at all ("", or a value this version never wrote), which is the
// "unset" state -- translating that would invent an override.
func ruleFromLegacy(mode, dir, suffix string) (DirSpec, bool) {
	switch strings.TrimSpace(mode) {
	case "same":
		return DirSpec{Mode: OutputSame}, true
	case "sibling":
		return DirSpec{Mode: OutputSibling, Suffix: orDefaultSuffix(suffix), KeepTree: true}, true
	case "custom":
		return DirSpec{Mode: OutputCustom, Dir: strings.TrimSpace(dir)}, true
	case "mirror":
		// 旧代码把"指定目录"和"指定目录 + 保留子目录"做成两个模式，现在是一个
		// 模式加一个开关。
		return DirSpec{Mode: OutputCustom, Dir: strings.TrimSpace(dir), KeepTree: true}, true
	}
	return DirSpec{}, false
}

// specFromExpr translates one 第 34 ~ 38 批 的目录表达式.
//
// 那时表达式里可以写变量（{dirPath:-1}\123_{rootName} 之类），靠一次性拼字符串
// 求值；现在只有"模式 + 字段"四种选择，装不下的表达式一律退到同级目录。之所以往
// 外退而不是往里退：产物落在源文件夹外面是这里唯一不能让的事，落进源文件夹的文件
// 会在下一次扫描里被当成输入再吃一遍。
func specFromExpr(path string, keepTree bool) DirSpec {
	p := strings.TrimSpace(path)
	if p == "" {
		return DirSpec{Mode: OutputSame}
	}
	if !strings.ContainsRune(p, '{') {
		return DirSpec{Mode: OutputCustom, Dir: p, KeepTree: keepTree}
	}
	// "{rootDir}_out" / "{rootDir}_done" —— 同级目录最常用的那种写法，正好就是
	// 现在这个模式加一个后缀。
	if rest, ok := strings.CutPrefix(p, "{rootDir}"); ok && !strings.ContainsRune(rest, '{') {
		return DirSpec{Mode: OutputSibling, Suffix: orDefaultSuffix(rest), KeepTree: keepTree}
	}
	return DirSpec{Mode: OutputSibling, Suffix: DefaultOutputSuffix, KeepTree: true}
}

// orDefaultSuffix keeps a suffix a user actually typed and fills the shipped one
// in when the old file left it blank: 同级目录 没有后缀就和源目录同名了。
func orDefaultSuffix(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return DefaultOutputSuffix
}

// adoptSection is the one place a legacy rule becomes a DirSpec.
//
// 有 path 键就一定是第 34 ~ 38 批写的，直接翻：那个键在这一版里不存在，而光看
// 解出来的字段会被 keepTree 骗过去 —— 它是两版都有的键，一个只写了
// {"path":"","keepTree":true} 的文件解出来也是非空的。
func adoptSection(dst *DirSpec, spec *legacySpec, rule legacyRule) {
	if spec != nil && spec.Path != nil {
		*dst = specFromExpr(*spec.Path, spec.KeepTree)
		return
	}
	// 没有 path 键就只能看三元组，而三元组翻译不出"没设置过"，所以目标字段还空着
	// 才动手 —— 一个这一版写的文件不该被老键改回去。
	if !dst.IsZero() {
		return
	}
	if got, ok := ruleFromLegacy(rule.Mode, rule.Dir, rule.Suffix); ok {
		*dst = got
	}
}

// adoptLegacyDirs rewrites the directory fields of a freshly loaded list. It
// re-reads the file because the old keys are not part of Template any more.
func adoptLegacyDirs(list []Template) {
	if len(list) == 0 {
		return
	}
	b, err := os.ReadFile(TemplatesPath())
	if err != nil {
		return
	}
	var raw []legacyTemplate
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) != len(list) {
		return
	}
	for i := range list {
		adoptLegacyTemplate(&list[i], raw[i])
	}
}

func adoptLegacyTemplate(t *Template, old legacyTemplate) {
	adoptSection(&t.OutDirSpec, old.OutSpec, legacyRule{old.OutMode, old.OutDir, old.OutSuffix})
	// 第 34 批之前的文件用"outMode 有没有值"表达"这一段是不是我自己接管"（留空 =
	// 跟随全局）。现在这件事只由 OutputOverride 决定，而那时的文件里可能根本没有
	// 这个键——不补上，刚翻译出来的规则会被 Effective 当成"跟随全局"直接丢掉。
	if !t.Global && !t.OutputOverride && old.OutSpec == nil && strings.TrimSpace(old.OutMode) != "" {
		t.OutputOverride = true
	}

	// 一个 nil 段本身就是"这一段跟随全局"，没有地方放翻译结果——也没什么可丢的。
	if t.Existing != nil && old.Existing != nil {
		adoptSection(&t.Existing.Dir, old.Existing.Dir, old.Existing.Dest)
	}
	if t.Filter != nil && old.Filter != nil {
		adoptSection(&t.Filter.Dir, old.Filter.Dir, old.Filter.Dest)
	}
	if t.Problems != nil && old.Problems != nil {
		adoptSection(&t.Problems.ErrorDir, old.Problems.ErrorDir, old.Problems.ErrorDest)
		adoptSection(&t.Problems.WarningDir, old.Problems.WarningDir, old.Problems.WarningDest)
	}
}
