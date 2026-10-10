package store

import "testing"

// 条件怎么比 —— 这一层是纯函数，和文件系统无关，所以它跟着规则一起住在 store 里
// （`DirFilter` 用同一组条件，而它要被设置文件保存）。

func TestMatchName(t *testing.T) {
	cases := []struct {
		name   string
		filter NameFilter
		want   bool
	}{
		{"前缀命中", NameFilter{FilterPrefix, "ffmpeg__"}, true},
		{"前缀不命中", NameFilter{FilterPrefix, "xyz"}, false},
		{"后缀命中", NameFilter{FilterSuffix, "__a"}, true},
		{"包含命中", NameFilter{FilterContains, "mpeg"}, true},
		{"包含不命中", NameFilter{FilterContains, "h264"}, false},
		// 不区分大小写：Windows 上 H265 / h265 是同一个目录。
		{"大小写不敏感", NameFilter{FilterContains, "FFMPEG__A"}, true},
		{"通配符", NameFilter{FilterGlob, "ffmpeg__*"}, true},
		{"通配符问号", NameFilter{FilterGlob, "ffmpeg__?"}, true},
		{"通配符不命中", NameFilter{FilterGlob, "*.mov"}, false},
		// 前缀里写了通配符也照样当通配符用 —— 用户不必先判断自己算哪一档。
		{"前缀里带通配符", NameFilter{FilterPrefix, "ff*_"}, true},
		{"包含里带通配符", NameFilter{FilterContains, "m*eg__a"}, true},
		{"后缀里带通配符", NameFilter{FilterSuffix, "_*a"}, true},
		// 正在输入时的半截模式当"没匹配"，不是报错。
		{"半截模式不算错", NameFilter{FilterGlob, "["}, false},
		{"空条件不匹配任何东西", NameFilter{FilterPrefix, "  "}, false},
	}
	for _, tc := range cases {
		if got := MatchName("ffmpeg__a", tc.filter); got != tc.want {
			t.Errorf("%s: %+v -> %v, want %v", tc.name, tc.filter, got, tc.want)
		}
	}
}

// 多条条件：满足全部 = 逐层收窄（用户原话"先过滤 ffmpeg__* 之后我再过滤包含
// h265"），满足任意 = 命中一条就算。两种都要有，判据只在 matchAll 一个位上。
func TestMatchNameAll(t *testing.T) {
	filters := []NameFilter{
		{FilterPrefix, "ffmpeg__"},
		{FilterSuffix, "_a"},
	}
	for _, tc := range []struct {
		name      string
		matchAll  bool
		wantA     bool // ffmpeg__a —— 两条都满足
		wantH265  bool // h265 —— 只满足一条
		wantFFB   bool // ffmpeg__b —— 只满足一条
		wantOther bool // other —— 一条都不满足
	}{
		{"满足全部", true, true, false, false, false},
		{"满足任意", false, true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range []struct {
				dir  string
				want bool
			}{
				{"ffmpeg__a", tc.wantA},
				{"h265", tc.wantH265},
				{"ffmpeg__b", tc.wantFFB},
				{"other", tc.wantOther},
			} {
				if got := MatchNameAll(c.dir, filters, tc.matchAll); got != c.want {
					t.Errorf("%s -> %v, want %v", c.dir, got, c.want)
				}
			}
		})
	}
	// 一条都没填 = 不筛选，任何目录都算命中（否则"没填条件"会变成"什么都选不上"）。
	if !MatchNameAll("随便", nil, true) || !MatchNameAll("随便", []NameFilter{{FilterPrefix, ""}}, false) {
		t.Error("空条件应当等同于不筛选")
	}
}

// 「开着的开关 + 一条空条件」不是"什么都选不上"，是"不筛选"。
//
// 面板打开时就是这个样子：启用是开着的，条件行还是空的。要是 Active 只看开关，用户
// 选完目录会一个文件都收不到 —— 而那看起来像过滤功能坏了。
func TestDirFilterIsOnlyActiveWhenItHasSomethingToMatchOn(t *testing.T) {
	cases := []struct {
		name string
		f    DirFilter
		want bool
	}{
		{"默认关上", DirFilter{}, false},
		{"开着但没条件", DirFilter{NameRules: NameRules{Enabled: true}}, false},
		{"开着但只有空条件", DirFilter{NameRules: NameRules{Enabled: true, Filters: []NameFilter{{FilterPrefix, "  "}}}}, false},
		{"关着但有条件", DirFilter{NameRules: NameRules{Filters: []NameFilter{{FilterPrefix, "ffmpeg__"}}}}, false},
		{"开着且有条件", DirFilter{NameRules: NameRules{Enabled: true, Filters: []NameFilter{{FilterPrefix, "ffmpeg__"}}}}, true},
	}
	for _, tc := range cases {
		if got := tc.f.Active(); got != tc.want {
			t.Errorf("%s: Active() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Normalize 的活儿：丢掉空行、把认不出的模式退回包含、nil 换成空切片。
//
// 空行留着没有意义，却会在下次打开面板时凭空多出几行；认不出的模式要是原样留着，
// 那条条件就静默失效了 —— 面板上看着有一条，实际什么都不筛。
func TestDirFilterNormalize(t *testing.T) {
	f := DirFilter{NameRules: NameRules{
		Enabled: true,
		Exclude: true,
		Filters: []NameFilter{
			{FilterPrefix, ""},
			{FilterContains, "  h265  "},
			{FilterPrefix, "   "},
			{"看都没看过这个模式", "batch"},
			{FilterSuffix, "_out"},
		},
	}}
	f.Normalize()

	want := []NameFilter{
		{FilterContains, "h265"},
		{FilterContains, "batch"},
		{FilterSuffix, "_out"},
	}
	if len(f.Filters) != len(want) {
		t.Fatalf("Filters = %+v, want %+v", f.Filters, want)
	}
	for i, w := range want {
		if f.Filters[i] != w {
			t.Errorf("Filters[%d] = %+v, want %+v", i, f.Filters[i], w)
		}
	}
	// 开关是用户按的，Normalize 不动它 —— 勾了启用却发现它自己跳回去了，那是猜。
	if !f.Enabled {
		t.Error("Normalize 不该改 Enabled")
	}
	// 方向也是用户按的，而且它比开关更容易被"顺手重置"掉：设置文件每次读出来都会过
	// 一趟 Normalize，所以这里把它清了，就等于方向永远回不去"排除"。
	if !f.Exclude {
		t.Error("Normalize 不该改 Exclude")
	}
	if nil == f.Filters {
		t.Error("Filters 应当是空切片而不是 nil")
	}

	var empty DirFilter
	empty.Normalize()
	if empty.Filters == nil {
		t.Error("没有条件时也要给空切片，写进设置文件的是 [] 而不是 null")
	}
}

// 同一条条件，两个方向问的是同一件事的两面：命中就收 / 命中就不要。
//
// 这条判据住在 store 而不是遍历里，是因为它必须只有一份：面板预览、目录遍历、真加入
// 三条路都问 `TakeDir`。其中任何一条自己判一次，长出来的就是"预览说排除、真跑却收了"
// —— 而那种错只有把文件加进来之后才看得见。
func TestTakeDirBothDirections(t *testing.T) {
	prefix := []NameFilter{{FilterPrefix, "ffmpeg__"}}
	cases := []struct {
		name    string
		filters []NameFilter
		exclude bool
		dir     string
		want    bool
	}{
		{"收方向命中", prefix, false, "ffmpeg__a", true},
		{"收方向没命中", prefix, false, "h265", false},
		{"排方向命中", prefix, true, "ffmpeg__a", false},
		{"排方向没命中", prefix, true, "h265", true},
		// 一条条件都没填时两个方向都是"要"：面板上条件留着、值清空的那种状态不该把
		// 整个文件夹变成空的。
		{"没条件收", nil, false, "h265", true},
		{"没条件排", nil, true, "h265", true},
		{"只有空条件排", []NameFilter{{FilterContains, "  "}}, true, "h265", true},
	}
	for _, c := range cases {
		if got := TakeName(c.dir, c.filters, true, c.exclude); got != c.want {
			t.Errorf("%s: TakeName(%q, exclude=%v) = %v, want %v", c.name, c.dir, c.exclude, got, c.want)
		}
	}

	// 方向不参与"满足全部 / 任意"那一层：两个方向共用同一份匹配结果，只是拿它做什么
	// 不同。这里把它钉住，免得以后有人在排除方向里再写一遍匹配。
	multi := []NameFilter{{FilterPrefix, "ffmpeg__"}, {FilterContains, "h265"}}
	if TakeName("ffmpeg__a", multi, true, false) {
		t.Error("满足全部：ffmpeg__a 不含 h265，收方向不该要")
	}
	if !TakeName("ffmpeg__a", multi, true, true) {
		t.Error("满足全部：ffmpeg__a 不含 h265，排方向应当要")
	}
	if !TakeName("ffmpeg__a", multi, false, false) {
		t.Error("满足任意：命中 ffmpeg__ 就够了，收方向该要")
	}
}

// 「含子目录」反过来存成 TopOnly，这条名字上的反转不是随口起的。
//
// 老设置文件里没有这个键，JSON 读成 false。字段若叫 Recursive，老用户一升级就变成
// "只收这一层" —— 添加文件夹会静默漏掉整棵子树，而他们什么都没改过。零值必须等于
// 这个开关出现之前的行为，所以判据钉在"零值 ≠ 只看一层"上。
func TestDirFilterZeroValueStillDescends(t *testing.T) {
	var fresh DirFilter
	if fresh.TopOnly {
		t.Error("零值不该是「只看这一层」—— 老设置文件里没有这个键")
	}
	if !fresh.Recursive(true) {
		t.Error("零值 + 调用方要递归 = 递归（含子目录）")
	}
}

// 两个来源都只能往"更窄"的方向收：调用方说不递归就不递归；设置说只看这一层就只看
// 这一层。谁也不许把对方放宽。
func TestDirFilterRecursiveOnlyNarrows(t *testing.T) {
	cases := []struct {
		name   string
		top    bool
		caller bool
		want   bool
	}{
		{"默认 + 调用方递归", false, true, true},
		{"默认 + 调用方不递归", false, false, false},
		{"只看一层 + 调用方递归", true, true, false},
		{"只看一层 + 调用方不递归", true, false, false},
	}
	for _, c := range cases {
		d := DirFilter{TopOnly: c.top}
		if got := d.Recursive(c.caller); got != c.want {
			t.Errorf("%s: Recursive(%v) = %v, want %v", c.name, c.caller, got, c.want)
		}
	}
}

// 存下来的方案：名字就是身份，所以空名字丢掉、重名只留先出现的那个。
//
// 两个同名项会让下拉里出现两行分不清的选项（选哪一个都一样），而"存"也不知道该覆盖
// 谁。保存路径本来就按名字覆盖，重复只可能来自手改过的设置文件，这里替它收口。
func TestNormalizeProfiles(t *testing.T) {
	in := []FilterProfile{
		{
			Name: "  批次  ",
			Dirs: DirFilter{NameRules: NameRules{
				Exclude: true, Filters: []NameFilter{{FilterPrefix, "  ffmpeg__  "}},
			}},
		},
		{Name: "   "},
		{Name: ""},
		{Name: "批次", Dirs: DirFilter{TopOnly: true}},
		{Name: "cache", Dirs: DirFilter{NameRules: NameRules{Exclude: true}, TopOnly: true}},
	}
	got := normalizeProfiles(in)
	if len(got) != 2 {
		t.Fatalf("留下 %d 个方案（%+v），want 2", len(got), got)
	}
	if got[0].Name != "批次" {
		t.Errorf("第一个方案名 = %q, want 批次（名字要去掉首尾空白）", got[0].Name)
	}
	// 重名收口之后留下的是先出现的那个，而不是后一个半成品。
	if !got[0].Dirs.Exclude || got[0].Dirs.TopOnly {
		t.Errorf("重名时应当保留先出现的那一份，得到 %+v", got[0].Dirs)
	}
	if len(got[0].Dirs.Filters) != 1 || got[0].Dirs.Filters[0].Value != "ffmpeg__" {
		t.Errorf("方案里的条件也要归一化，得到 %+v", got[0].Dirs.Filters)
	}
	if got[1].Name != "cache" {
		t.Errorf("第二个方案名 = %q, want cache", got[1].Name)
	}
}

// 一套都不剩时补上一套空的：任务页那个下拉要有东西可选。
//
// 留零套的话，用户看到的是一排没有内容的控件。而"没有方案"和"有一套什么都不筛的方案"
// 在行为上是同一件事（都不过滤），在界面上却不是 —— 补空的比留零套少一个状态。
func TestNormalizeProfilesAlwaysLeavesOne(t *testing.T) {
	for _, in := range [][]FilterProfile{nil, {}, {{Name: "   "}}} {
		got := normalizeProfiles(in)
		if len(got) != 1 || got[0].Name != DefaultFilterName {
			t.Fatalf("normalizeProfiles(%+v) = %+v, want 一套「%s」", in, got, DefaultFilterName)
		}
		if got[0].Active() {
			t.Error("补上的那一套必须是空的，否则等于替用户开了过滤")
		}
	}
}

// 两组规则互不相干：目录那组的开关拨不动文件那组，反过来也一样。
//
// 用同一份条件回答"收哪几个目录"和"收哪几个文件"是这里最容易图省事的写法，而它会让
// 两边都写不准。把两组的行为分开钉住，改一边时另一边必须还在。
func TestProfileKeepsDirAndFileRulesIndependent(t *testing.T) {
	p := FilterProfile{
		Name:  "批次",
		Dirs:  DirFilter{NameRules: NameRules{Enabled: true, Filters: []NameFilter{{FilterPrefix, "batch"}}}},
		Files: NameRules{Enabled: true, Exclude: true, Filters: []NameFilter{{FilterSuffix, ".tmp"}}},
	}
	p.Normalize()
	if !p.Dirs.Active() || !p.Files.Active() {
		t.Fatalf("两组都该是生效的：%+v", p)
	}
	if !p.Dirs.Take("batch01") || p.Dirs.Take("h265") {
		t.Error("目录规则：命中 batch 前缀的才算")
	}
	if !p.Files.Take("clip.mp4") || p.Files.Take("x.tmp") {
		t.Error("文件规则：排除方向，后缀 .tmp 的不要，其余都要")
	}
	p.Dirs.Enabled = false
	if p.Dirs.Active() || !p.Files.Active() {
		t.Error("关掉目录规则不该影响文件规则")
	}
}

// 开关关掉的那一组就是"不过滤"，哪怕条件填得满满的。
//
// `TakeName` 只看条件，所以"这一组到底有没有在筛"必须由 `Take` 一起看开关。让每个
// 调用点自己拼 `if Active() && TakeName(...)` 的话，写三遍就会有一遍忘了看开关 ——
// 那时页面上开关是关的，文件却照旧被筛掉。
func TestTakeRespectsEnabled(t *testing.T) {
	r := NameRules{Enabled: false, Filters: []NameFilter{{FilterPrefix, "batch"}}, Exclude: true}
	if !r.Take("batch01") {
		t.Error("开关关着时 Exclude 也不该生效：这条本来就是「命中就不要」的条件")
	}
	r.Enabled = true
	if r.Take("batch01") {
		t.Error("开关打开后，排除方向命中就该不要")
	}
	// 零值组 = 不过滤。这正是"收的方向下不套这组规则"要的东西：传空的那个即可，
	// 不必再开一条代码路径。
	var none NameRules
	if !none.Take("随便") {
		t.Error("零值组应当什么都不筛")
	}
}

// 名字指向一套不存在的方案时退回第一套，而不是零值。
//
// 零值意味着"两组规则都关着" = 不过滤，表面上和"找不到"没什么区别；区别在于任务页那个
// 下拉要显示**哪一套在生效**，指着不存在的东西一行都显示不出来。
func TestPickProfileFallsBackToFirst(t *testing.T) {
	list := []FilterProfile{{Name: "批次"}, {Name: "cache"}}
	if got := PickProfile("cache", list); got.Name != "cache" {
		t.Errorf("按名字找 = %q, want cache", got.Name)
	}
	if got := PickProfile("CACHE", list); got.Name != "cache" {
		t.Errorf("名字比较不区分大小写，得到 %q", got.Name)
	}
	if got := PickProfile("早就删了", list); got.Name != "批次" {
		t.Errorf("找不到时 = %q, want 第一套（批次）", got.Name)
	}
	if got := PickProfile("批次", nil); got.Name != "" {
		t.Errorf("一套都没有时给零值，得到 %q", got.Name)
	}
}

// 方案名和说明都要收空白：说明是列表里那一行字，写进设置文件时带着换行和空格，
// 列表上就会多出一截空白，而"这一行到底写了什么"正是它存在的唯一理由。
//
// 它**不参与任何判断** —— 条件照旧只看模式、值、方向和满足度。
func TestProfileNormalizeTrimsNameAndDescription(t *testing.T) {
	p := FilterProfile{
		Name:        "  批次  ",
		Description: "  只收转码中间产物  \n",
		Dirs: DirFilter{NameRules: NameRules{
			Filters: []NameFilter{{Mode: FilterContains, Value: " batch "}, {Mode: FilterContains, Value: "  "}},
		}},
	}
	p.Normalize()
	if p.Name != "批次" {
		t.Errorf("Name = %q, want 批次", p.Name)
	}
	if p.Description != "只收转码中间产物" {
		t.Errorf("Description = %q, want 只收转码中间产物", p.Description)
	}
	if len(p.Dirs.Filters) != 1 {
		t.Fatalf("条件 = %+v, want 1 条（空值那行要丢掉）", p.Dirs.Filters)
	}
	if p.Active() {
		t.Error("两组都没开时不该算在筛")
	}
}
