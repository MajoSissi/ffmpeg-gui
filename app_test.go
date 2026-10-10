package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ffmpeggui/internal/engine"
	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// newEnqueueApp 是一个只够跑「加进来几条」的 App。
//
// runner 要 `Configure` 过：`finishAdd` 在真加进来东西之后会叫一次 `Preprobe`，而
// 它第一句就是 `prov.Binaries()` —— `Providers` 的零值里那几个字段都是 nil 函数，
// 直接调就是空指针。给一个"没有二进制"的回答正好：预探测会当场收摊，队列里的条数
// 才是这个测试要量的东西。
func newEnqueueApp(f store.DirFilter) *App {
	return newEnqueueAppWith(f, store.NameRules{})
}

// newEnqueueAppWith 同上，但多给一组文件规则。
//
// 过滤现在是"一套方案两组规则"，而这两个测试文件里绝大多数用例只关心目录那一组 ——
// 所以装配这件事收在这两个函数里，用例本身还是只写自己关心的那几个参数。
func newEnqueueAppWith(dirs store.DirFilter, files store.NameRules) *App {
	a := &App{runner: engine.NewRunner()}
	a.runner.Configure(engine.Providers{Binaries: func() media.Binaries { return media.Binaries{} }}, nil, nil)
	a.settings = store.Settings{
		FilterProfiles: []store.FilterProfile{{Name: store.DefaultFilterName, Dirs: dirs, Files: files}},
		ActiveFilter:   store.DefaultFilterName,
	}
	return a
}

// The tray hover text is the thing the user sees when the window is out of the
// way, and it was wrong in two ways at once: it only ever got written on 开始 and
// 保存设置 (every other queue change went straight to a.emit, bypassing it), and
// it only got written while Total > 0, so clearing the queue left the last count
// frozen on the icon. Both are pinned here.
func TestTrayTooltipTracksQueueState(t *testing.T) {
	cases := []struct {
		name string
		st   engine.Stats
		want string
	}{
		{
			name: "空队列回落到空闲标题",
			st:   engine.Stats{},
			want: "",
		},
		{
			name: "刚拖进来还没开始",
			st:   engine.Stats{Total: 9, Pending: 9},
			want: "FFmpeg GUI — 0/9 完成 · 未开始",
		},
		{
			name: "处理中",
			st:   engine.Stats{Total: 9, Pending: 6, Running: 3, Started: true},
			want: "FFmpeg GUI — 0/9 完成 · 3 处理中",
		},
		{
			name: "暂停优先于处理中",
			st:   engine.Stats{Total: 9, Pending: 6, Running: 3, Started: true, Paused: true},
			want: "FFmpeg GUI — 0/9 完成 · 已暂停",
		},
		{
			// Removed rows count as finished, exactly like the progress bar does:
			// Stats.Finished is the one sum both of them read.
			name: "跳过与已取消都算完成",
			st:   engine.Stats{Total: 4, Done: 1, Skipped: 1, Canceled: 1, Failed: 1, Started: true},
			want: "FFmpeg GUI — 4/4 完成",
		},
		{
			name: "全部完成",
			st:   engine.Stats{Total: 2, Done: 1, Warning: 1, Started: true},
			want: "FFmpeg GUI — 2/2 完成",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trayTooltip(tc.st); got != tc.want {
				t.Errorf("trayTooltip = %q, want %q", got, tc.want)
			}
		})
	}
}

// The hover text is truncated by the shell at 128 UTF-16 units, and a tooltip that
// silently loses its tail is worse than a short one.
func TestTrayTooltipIsShortEnoughForTheShell(t *testing.T) {
	st := engine.Stats{Total: 100000, Done: 99999, Running: 1, Started: true}
	got := trayTooltip(st)
	if n := len([]rune(got)); n > 100 {
		t.Errorf("tooltip is %d chars, too long for the notification area: %q", n, got)
	}
	if !strings.Contains(got, "处理中") {
		t.Errorf("the queue state is missing from %q", got)
	}
}

// Finished is the one definition of "done" the progress bar and the tooltip share.
func TestStatsFinishedCountsEveryTerminalStatus(t *testing.T) {
	st := engine.Stats{Done: 1, Warning: 2, Failed: 3, Canceled: 4, Skipped: 5, Filtered: 6, Pending: 7, Running: 8}
	if got := st.Finished(); got != 21 {
		t.Errorf("Finished = %d, want 21 (pending and running must not count)", got)
	}
}

// --------------------------------------------------------------- history filter

func day(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.Local)
}

func recordIDs(list []store.Record) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.ID)
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The date range has to agree with the timestamps the table puts on screen, and
// the end date has to include its own last minute -- a filter that quietly drops
// the evening of the day you picked reads as missing records.
func TestHistoryDateRangeIsInclusiveAndUsesTheDisplayedTimestamp(t *testing.T) {
	recs := []store.Record{
		// Crosses midnight: dated by when it finished, not when it started.
		{ID: "crossed", StartedAt: day(2026, 5, 1, 23, 0), EndedAt: day(2026, 5, 2, 0, 5)},
		{ID: "early", StartedAt: day(2026, 5, 2, 0, 0)},
		{ID: "late", StartedAt: day(2026, 5, 2, 23, 59), EndedAt: day(2026, 5, 2, 23, 59)},
		{ID: "next", StartedAt: day(2026, 5, 3, 0, 0)},
		{ID: "stale", StartedAt: day(2026, 4, 30, 10, 0)},
	}
	cases := []struct {
		name string
		q    HistoryQuery
		want []string
	}{
		{
			name: "单日：两端都含当天",
			q:    HistoryQuery{From: "2026-05-02", To: "2026-05-02"},
			want: []string{"late", "crossed", "early"},
		},
		{
			name: "只有起始日期",
			q:    HistoryQuery{From: "2026-05-03"},
			want: []string{"next"},
		},
		{
			name: "只有结束日期",
			q:    HistoryQuery{To: "2026-05-01"},
			want: []string{"stale"},
		},
		{
			name: "跨天区间",
			q:    HistoryQuery{From: "2026-05-01", To: "2026-05-03"},
			want: []string{"next", "late", "crossed", "early"},
		},
		{
			// A half-filled or hand-typed value must not blank the list: an
			// unusable date simply has no bound.
			name: "填不成日期就当作没有这个边界",
			q:    HistoryQuery{From: "2026-5-2", To: "不是日期"},
			want: []string{"next", "late", "crossed", "early", "stale"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recordIDs(filterHistory(recs, tc.q))
			if !equalIDs(got, tc.want) {
				t.Errorf("filterHistory = %v, want %v", got, tc.want)
			}
		})
	}
}

// 排序和筛选必须看同一个时间戳，否则「按日期筛出 5 月 2 日」和「按时间排序」会
// 给出两个互相矛盾的顺序。
func TestHistorySortTogglesDirection(t *testing.T) {
	recs := []store.Record{
		{ID: "old", StartedAt: day(2026, 5, 1, 8, 0)},
		{ID: "new", StartedAt: day(2026, 5, 3, 8, 0)},
		{ID: "mid", StartedAt: day(2026, 5, 2, 8, 0)},
	}
	if got := recordIDs(filterHistory(recs, HistoryQuery{})); !equalIDs(got, []string{"new", "mid", "old"}) {
		t.Errorf("默认应最新在前，得到 %v", got)
	}
	if got := recordIDs(filterHistory(recs, HistoryQuery{Sort: "newest"})); !equalIDs(got, []string{"new", "mid", "old"}) {
		t.Errorf("newest 应最新在前，得到 %v", got)
	}
	if got := recordIDs(filterHistory(recs, HistoryQuery{Sort: "oldest"})); !equalIDs(got, []string{"old", "mid", "new"}) {
		t.Errorf("oldest 应最早在前，得到 %v", got)
	}
}

// --------------------------------------------------------------- delete records

// 删除记录是破坏性的，所以三件事都要成立：只删点名的那些、剩下的原样不动、
// 磁盘上那一份也跟着少掉。
func TestDeleteRecordsKeepsTheRest(t *testing.T) {
	store.SetDataDir(t.TempDir())
	a := &App{history: []store.Record{{ID: "a"}, {ID: "b"}, {ID: "c"}}}

	n, err := a.DeleteRecords([]string{"b"})
	if err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}
	if n != 1 {
		t.Errorf("删掉了 %d 条，want 1", n)
	}
	if got := recordIDs(a.history); !equalIDs(got, []string{"a", "c"}) {
		t.Errorf("剩下的记录 = %v, want [a c]", got)
	}
	if got := recordIDs(store.LoadHistory()); !equalIDs(got, []string{"a", "c"}) {
		t.Errorf("落盘的记录 = %v, want [a c]", got)
	}
}

// 一个都没命中的时候不许写盘：用户看到的列表和文件都不该有任何变化。
func TestDeleteRecordsWithNothingToDoWritesNothing(t *testing.T) {
	store.SetDataDir(t.TempDir())
	a := &App{history: []store.Record{{ID: "a"}}}

	n, err := a.DeleteRecords([]string{"zz", ""})
	if err != nil || n != 0 {
		t.Fatalf("DeleteRecords = %d, %v; want 0, nil", n, err)
	}
	if got := recordIDs(a.history); !equalIDs(got, []string{"a"}) {
		t.Errorf("记录被改动了: %v", got)
	}
	if _, statErr := os.Stat(store.HistoryPath()); !os.IsNotExist(statErr) {
		t.Errorf("没有删除任何记录却写了 history.json")
	}
}

// 「定位源文件」找的是列表上那条路径。源文件被「已处理过的文件」搬走之后就只剩
// 落点，所以候选的顺序是有意义的：先列表上那条，再落点。反过来的话，每一行都会
// 先去开一个多数时候不存在的地方，而真正的文件就在脚底下。
func TestLocateCandidatesTriesTheListedPathFirst(t *testing.T) {
	cases := []struct {
		name              string
		primary, fallback string
		want              []string
	}{
		{"只有列表上那条", `D:\a\x.mp4`, "", []string{`D:\a\x.mp4`}},
		{"搬走过：列表上那条在前，落点在后", `D:\a\x.mp4`, `D:\done\x.mp4`, []string{`D:\a\x.mp4`, `D:\done\x.mp4`}},
		{"两条一样就只说一次", `D:\a\x.mp4`, `D:\a\x.mp4`, []string{`D:\a\x.mp4`}},
		{"列表上那条是空的，只剩落点", "", `D:\done\x.mp4`, []string{`D:\done\x.mp4`}},
		{"两个都空", "", "", nil},
		{"空白不算路径", "  ", "\t", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := locateCandidates(tc.primary, tc.fallback); !equalIDs(got, tc.want) {
				t.Errorf("locateCandidates(%q, %q) = %v, want %v", tc.primary, tc.fallback, got, tc.want)
			}
		})
	}
}

// 存在性必须在 Go 侧判：Explorer 对不存在的路径不报错，只是默默打开「文档」，
// 那和定位成功长得一模一样。所以两条候选都不在的时候，这里要给出错误，而不是把
// 一个空路径丢给 Shell 去演一场空成功。
func TestLocateReportsMissingFilesInsteadOfOpeningTheShell(t *testing.T) {
	a := &App{}
	missing := filepath.Join(t.TempDir(), "gone.mp4")
	elsewhere := filepath.Join(t.TempDir(), "moved.mp4")

	got, err := a.Locate(missing, "")
	if err == nil {
		t.Fatalf("定位一个不存在的文件却成功了: %+v", got)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("错误里没说是哪条路径不在: %v", err)
	}

	if _, err := a.Locate(missing, elsewhere); err == nil {
		t.Fatal("两条候选都不在却成功了")
	} else if !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), elsewhere) {
		// 两个地方都要说出来：只说一个的话，用户没法判断是文件没了还是只是搬走了。
		t.Errorf("错误里没有把两个地方都说出来: %v", err)
	}

	if _, err := a.Locate("", ""); err == nil {
		t.Error("没有可定位的路径却成功了")
	}
}

// 「路径测试」要在用户还在打字的阶段就答出来，所以它只按路径算、不读文件。它必须
// 走和 runner 同一对函数（OutputRoot / ResolveOutput），否则这个面板说的和真跑一遍
// 做的会不一样 —— 而那正是这个面板存在的理由。
func TestPreviewPathsResolvesBothDirectories(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.mp4")
	_ = os.WriteFile(src, []byte("x"), 0o644)

	a := &App{}
	a.templates = []store.Template{store.DefaultGlobalTemplate()}

	// 跟随全局：默认规则是 {rootDir}_out，单个文件时 {rootDir} 就是它所在的目录。
	got, err := a.PreviewPaths(store.Template{}, src)
	if err != nil {
		t.Fatal(err)
	}
	if want := dir + "_out"; got.OutputDir != want {
		t.Errorf("OutputDir = %q, want %q", got.OutputDir, want)
	}
	if want := filepath.Join(dir+"_out", "a.mp4"); got.OutputPath != want {
		t.Errorf("OutputPath = %q, want %q", got.OutputPath, want)
	}
	if got.SrcDir != dir {
		t.Errorf("SrcDir = %q, want %q", got.SrcDir, dir)
	}

	// 打开「与全局不同」但什么都不填：目录回到源文件所在目录，名称回到源文件名。
	// 结果是输入自己，所以那只防覆盖的手把它让开成 a_out.mp4。
	own := store.Template{OutputOverride: true, Name: "自己一套"}
	got2, err := a.PreviewPaths(own, src)
	if err != nil {
		t.Fatal(err)
	}
	if got2.OutputDir != dir {
		t.Errorf("留空应当留在源文件目录，得到 %q", got2.OutputDir)
	}
	if want := filepath.Join(dir, "a_out.mp4"); got2.OutputPath != want {
		t.Errorf("OutputPath = %q, want %q", got2.OutputPath, want)
	}
}

// 用户点名要的那条：给目录名加前缀。同级目录 + 前缀 123_ 就是 E:\Bili → E:\123_Bili。
// 这个面板是唯一能看这条规则真算成什么的地方，所以它得跟着一起对。
func TestPreviewPathsPrefixesTheSourceFolderName(t *testing.T) {
	a := &App{}
	a.templates = []store.Template{store.DefaultGlobalTemplate()}
	src := filepath.Join("E:\\", "Bili", "1.mp4")

	for _, tc := range []struct {
		name string
		spec store.DirSpec
		dir  string
	}{
		{"只有前缀", store.DirSpec{Mode: store.OutputSibling, Prefix: "123_"}, filepath.Join("E:\\", "123_Bili")},
		{"前缀和后缀", store.DirSpec{Mode: store.OutputSibling, Prefix: "123_", Suffix: "_out"},
			filepath.Join("E:\\", "123_Bili_out")},
		// 两边都留空不能落回源目录：产物会和输入挤在同一个文件夹里。
		{"都留空", store.DirSpec{Mode: store.OutputSibling}, filepath.Join("E:\\", "Bili_out")},
	} {
		got, err := a.PreviewPaths(store.Template{OutputOverride: true, OutDirSpec: tc.spec}, src)
		if err != nil {
			t.Fatal(err)
		}
		if got.OutputDir != tc.dir {
			t.Errorf("%s: OutputDir = %q, want %q", tc.name, got.OutputDir, tc.dir)
		}
		if want := filepath.Join(tc.dir, "1.mp4"); got.OutputPath != want {
			t.Errorf("%s: OutputPath = %q, want %q", tc.name, got.OutputPath, want)
		}
	}
}

// 不是文件路径的输入要说清楚，不能编出一条看着挺像的输出路径。
func TestPreviewPathsRejectsNonFileInput(t *testing.T) {
	a := &App{}
	a.templates = []store.Template{store.DefaultGlobalTemplate()}
	dir := t.TempDir()

	for _, in := range []string{"", "   ", dir + string(filepath.Separator)} {
		if _, err := a.PreviewPaths(store.Template{}, in); err == nil {
			t.Errorf("输入 %q 应当被拒绝", in)
		}
	}

	// 文件不存在不是错误：用户完全可以在整理之前先看一眼路径会落到哪 —— 但这事
	// 得说出来，否则面板看起来像是读到了文件。
	missing := filepath.Join(dir, "not-there.mp4")
	got, err := a.PreviewPaths(store.Template{}, missing)
	if err != nil {
		t.Fatalf("文件不存在不该失败: %v", err)
	}
	if got.OutputPath == "" {
		t.Error("文件不存在时也该算出输出路径")
	}
	if len(got.Notices) == 0 {
		t.Error("文件不存在时应当在 Notices 里说出来")
	}
}

// 存一套过滤方案：同名就是"改这一套"，不是"再加一套"。
//
// 名字是这套方案唯一的身份（没有 id），所以覆盖必须发生在**原来的位置**上：追加
// 到末尾的话，用户改一下常用的那套，它的下拉位置就跳到最下面去了。大小写不敏感
// 是因为下拉里那两个名字看起来一模一样。
func TestPutProfileOverwritesInPlace(t *testing.T) {
	base := []store.FilterProfile{
		{Name: "批次", Dirs: store.DirFilter{NameRules: store.NameRules{Exclude: true}}},
		{Name: "cache", Dirs: store.DirFilter{TopOnly: true}},
	}

	got := putProfile(base, "", store.FilterProfile{Name: "cache", Dirs: store.DirFilter{TopOnly: true}})
	if len(got) != len(base) {
		t.Fatalf("同名应当覆盖而不是追加，得到 %d 项", len(got))
	}
	if got[1].Name != "cache" || !got[1].Dirs.TopOnly {
		t.Errorf("第 2 项没被换掉：%+v", got[1])
	}
	if got[0].Name != "批次" {
		t.Errorf("第 1 项不该动，得到 %q", got[0].Name)
	}

	// 大小写不敏感：下拉里 "CACHE" 和 "cache" 看起来是同一个名字，存下去必须是覆盖
	// 而不是再多一项。
	got = putProfile(base, "", store.FilterProfile{Name: "CACHE"})
	if len(got) != len(base) || got[1].Name != "CACHE" {
		t.Fatalf("大小写不同的同名应当覆盖：%+v", got)
	}

	// 新名字追加到末尾，原有顺序不动。
	got = putProfile(base, "", store.FilterProfile{Name: "h265"})
	if len(got) != 3 || got[2].Name != "h265" {
		t.Fatalf("新方案应当追加到末尾：%+v", got)
	}
	if got[0].Name != "批次" || got[1].Name != "cache" {
		t.Errorf("追加不该改动已有顺序：%+v", got)
	}
}

// 改名：旧名字那一格就是它的新家。
//
// 没有这一步的话「重命名」会变成"多一套、旧的那套还在" —— 名字就是方案的标识，一个
// 名字指两套方案的时候，任务页那个下拉里会出现两行分不清谁是谁的东西。
func TestPutProfileRenamesInPlace(t *testing.T) {
	base := []store.FilterProfile{{Name: "批次"}, {Name: "cache"}, {Name: "h265"}}

	got := putProfile(base, "cache", store.FilterProfile{Name: "缓存", Dirs: store.DirFilter{TopOnly: true}})
	if len(got) != 3 {
		t.Fatalf("改名不该多出一项：%+v", got)
	}
	if got[1].Name != "缓存" || !got[1].Dirs.TopOnly {
		t.Errorf("新名字没落在旧名字那一格：%+v", got)
	}
	if got[0].Name != "批次" || got[2].Name != "h265" {
		t.Errorf("改名不该动别的方案：%+v", got)
	}

	// 改成一个已经存在的名字：两边合成一套，留下的还是**旧名字那一格**。
	got = putProfile(base, "cache", store.FilterProfile{Name: "批次"})
	if len(got) != 2 || got[0].Name != "批次" || got[1].Name != "h265" {
		t.Fatalf("改成已有的名字应当合并成一套：%+v", got)
	}

	// 旧名字根本不在表里（别处删掉了）：按新名字走，不要凭空吃掉一个位置。
	got = putProfile(base, "早就删了", store.FilterProfile{Name: "cache"})
	if len(got) != 3 || got[1].Name != "cache" {
		t.Fatalf("旧名字对不上时应当按新名字覆盖：%+v", got)
	}
}

// 「含子目录」真的走到底：它管的是"往下收多深"，所以过滤整个关掉时同样算数。
//
// 这里量的是**加进来的任务数**，不是 `DirFilter.Recursive` 的返回值 —— 两边都各自
// 有测试，而这两段之间那条路（`App.enqueue` 把设置读出来、盖在调用方给的那个
// recursive 上）恰恰是最容易少写一句、又最不容易看出来的地方：面板上开关灰着、
// 拖进来的文件夹却还是整棵。
func TestEnqueueHonoursTopOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("top.mp4")                // 直接放在所选目录里的
	write("batch__01/x1.mp4")       //
	write("batch__01/h265/x2.mkv")  // 命中，但只看一层时够不着
	write("batch__02/x3.mp4")       //
	write("h265-archive/x4.mp4")    // 唯一一个直接子目录里命中的
	write("other/deep/h265/x5.mp4") //

	h265 := []store.NameFilter{{Mode: store.FilterContains, Value: "h265"}}

	for _, tc := range []struct {
		name   string
		filter store.DirFilter
		added  int
		why    string
	}{
		{
			name:   "收 + 含子目录",
			filter: store.DirFilter{NameRules: store.NameRules{Enabled: true, Filters: h265}},
			added:  3, // batch__01/h265 + h265-archive + other/deep/h265
		},
		{
			name:   "收 + 只看这一层",
			filter: store.DirFilter{NameRules: store.NameRules{Enabled: true, Filters: h265}, TopOnly: true},
			added:  1, // 只有 h265-archive 在直接子目录里
			why:    "只看一层时够不着更深的那些",
		},
		{
			name:   "排 + 含子目录",
			filter: store.DirFilter{NameRules: store.NameRules{Enabled: true, Filters: h265, Exclude: true}},
			added:  3, // top.mp4 + batch__01/x1.mp4 + batch__02/x3.mp4
			why:    "整个文件夹减去三棵命中的子树",
		},
		{
			name:   "排 + 只看这一层",
			filter: store.DirFilter{NameRules: store.NameRules{Enabled: true, Filters: h265, Exclude: true}, TopOnly: true},
			added:  1, // 根本没往里看，条件无从生效，收的就是根那一层
			why:    "不递归时一个子目录都不会被跳过",
		},
	} {
		a := newEnqueueApp(tc.filter)
		// 调用方（拖入文件夹）永远是递归的：收窄只能来自设置那一份。
		res := a.enqueue([]string{root}, true, true)
		if len(res.Errors) != 0 {
			t.Fatalf("%s: errors = %v", tc.name, res.Errors)
		}
		if res.Added != tc.added {
			t.Errorf("%s: added = %d, want %d（%s）", tc.name, res.Added, tc.added, tc.why)
		}
		if got := len(a.runner.Jobs()); got != tc.added {
			t.Errorf("%s: 队列里 %d 条，want %d", tc.name, got, tc.added)
		}
	}

	// 条件留着、开关关掉：整个文件夹照收。这是「启用」存在的理由，而它同时说明
	// 「只看这一层」**不归「启用」管** —— 它管的是往下收多深，下面两档说的就是这个。
	a := newEnqueueApp(store.DirFilter{NameRules: store.NameRules{Filters: h265}})
	if res := a.enqueue([]string{root}, true, true); res.Added != 6 {
		t.Errorf("过滤没启用时应当收整棵树（top.mp4 + 五个）: added = %d, want 6", res.Added)
	}

	a = newEnqueueApp(store.DirFilter{NameRules: store.NameRules{Filters: h265}, TopOnly: true})
	if res := a.enqueue([]string{root}, true, true); res.Added != 1 {
		t.Errorf("过滤关掉时「只看这一层」照样算数: added = %d, want 1（top.mp4）", res.Added)
	}
}

// 文件规则在**唯一的入口**上生效：扫出来的文件和点名的文件都算。
//
// 两条路各判一次的话，"拖进来一个文件"和"拖进来一个文件夹、里面正是同一个文件"会有
// 两种结果 —— 而用户看不出这两种情况有什么不同。所以这条测试同时压两个方向。
func TestEnqueueAppliesFileRules(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a1.mp4")
	write("b2.mp4")
	write("sub/c3.mp4")

	// 排方向：名字里有 b 的不要。目录规则一组都不填 —— 目录全收、文件筛掉一个，
	// 这正是"两组独立"最常见的用法。
	noB := store.NameRules{
		Enabled: true, Exclude: true,
		Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "b"}},
	}
	a := newEnqueueAppWith(store.DirFilter{}, noB)
	res := a.enqueue([]string{root}, true, true)
	if len(res.Errors) != 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	if res.Added != 2 {
		t.Errorf("排掉 b2.mp4 后应当剩 2 个: added = %d", res.Added)
	}

	// 同一个文件被**点名**加进来时，规则还是一样 —— 否则"文件过滤"只在扫目录时
	// 有效，而那件事用户看不出来。
	res = a.enqueue([]string{filepath.Join(root, "b2.mp4")}, false, true)
	if res.Added != 0 {
		t.Errorf("点名的文件也要过文件规则: added = %d, want 0", res.Added)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "被文件过滤条件排除") {
		t.Errorf("errors = %v, want 一条说明被文件规则排除的", res.Errors)
	}

	// 收方向：只要名字里带 c 的。
	onlyC := store.NameRules{
		Enabled: true,
		Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "c"}},
	}
	a = newEnqueueAppWith(store.DirFilter{}, onlyC)
	res = a.enqueue([]string{root}, true, true)
	if res.Added != 1 {
		t.Errorf("收方向应当只留 sub/c3.mp4: added = %d", res.Added)
	}
	if jobs := a.runner.Jobs(); len(jobs) != 1 || !strings.HasSuffix(jobs[0].Input, "c3.mp4") {
		t.Errorf("收进来的是 %v，want 只有 c3.mp4", jobs)
	}

	// 一个都没进来时要说明是文件规则干的 —— 否则队列空着而界面上没有任何解释。
	none := store.NameRules{
		Enabled: true,
		Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "zzz"}},
	}
	a = newEnqueueAppWith(store.DirFilter{}, none)
	res = a.enqueue([]string{root}, true, true)
	if res.Added != 0 {
		t.Fatalf("added = %d, want 0", res.Added)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "被文件过滤条件排除") {
		t.Errorf("errors = %v, want 一条说明几个文件被文件规则排除的", res.Errors)
	}
}

// 任务页上下拉选哪套，就一直用哪套：它写设置文件，下次打开从它开始。
//
// 没有第二份"默认那套"和它分庭抗礼 —— 界面上分不出来的东西，模型里也不该有两份。
// 落盘的那条回归是"关掉程序再打开还是这套"，而不是"设置文件里的默认没被动过"。
func TestSetActiveFilterPersists(t *testing.T) {
	defer store.SetDataDir(store.DataDir())
	store.SetDataDir(t.TempDir())

	prof := func(name, want string) store.FilterProfile {
		return store.FilterProfile{
			Name: name,
			Dirs: store.DirFilter{NameRules: store.NameRules{
				Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: want}},
			}},
		}
	}
	a := &App{runner: engine.NewRunner()}
	a.settings = store.Settings{
		FilterProfiles: []store.FilterProfile{prof("默认", "a"), prof("只要 b", "b")},
		ActiveFilter:   "默认",
	}
	if err := store.SaveSettings(a.settings); err != nil {
		t.Fatal(err)
	}

	st, err := a.SetActiveFilter("只要 b", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "只要 b" || st.Profile.Name != "只要 b" {
		t.Fatalf("选择没生效：%+v", st)
	}
	if got := a.effectiveFilter().Name; got != "只要 b" {
		t.Errorf("队列读到的还是 %q，want 只要 b", got)
	}
	if got := store.LoadSettings().ActiveFilter; got != "只要 b" {
		t.Errorf("设置文件里 = %q, want 只要 b（下拉选的就是在用的，要落盘）", got)
	}

	// 名字对不上任何一套时退回第一套，而不是把生效方案指向一个不存在的东西。
	if st, _ = a.SetActiveFilter("早就删了", false); st.Profile.Name != "默认" {
		t.Fatalf("对不上的名字不该被记住：%+v", st)
	}
}

// 「不使用过滤」只在本次运行里：两组规则都不参与，但设置文件里记的还是上次选的那套。
//
// 判据是零值方案（两组都没开 → `NameRules.Take` 一律放行），不是另开一条"跳过过滤"
// 的代码路径 —— 另开一条，就会有"关掉开关"和"选中禁用"两种说法，而它们必须永远等价。
func TestFilterOffIsSessionOnly(t *testing.T) {
	defer store.SetDataDir(store.DataDir())
	store.SetDataDir(t.TempDir())

	a := &App{runner: engine.NewRunner()}
	a.settings = store.Settings{
		FilterProfiles: []store.FilterProfile{{
			Name:  "只要 h265",
			Dirs:  store.DirFilter{NameRules: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "h265"}}}},
			Files: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterSuffix, Value: ".tmp"}}, Exclude: true},
		}},
		ActiveFilter: "只要 h265",
	}
	if err := store.SaveSettings(a.settings); err != nil {
		t.Fatal(err)
	}

	st, err := a.SetActiveFilter("只要 h265", true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Off {
		t.Fatalf("状态里没有报出禁用：%+v", st)
	}
	// 界面上正显示着「不使用过滤」，交出去的规则却还是那套 = 两边在说不一致的话。
	if st.Profile.Dirs.Active() || st.Profile.Files.Active() {
		t.Errorf("禁用时交出去的规则还在筛：%+v", st.Profile)
	}
	eff := a.effectiveFilter()
	if eff.Dirs.Active() || eff.Files.Active() {
		t.Errorf("队列读到的还在筛：%+v", eff)
	}
	// 「含子目录」不跟过滤走：禁用时仍然是含子目录（零值 = 不 TopOnly）。
	if !eff.Dirs.Recursive(true) {
		t.Error("禁用时应当照旧含子目录")
	}
	// 设置文件一个字都没动 —— 下次打开还是那套。
	if got := store.LoadSettings().ActiveFilter; got != "只要 h265" {
		t.Errorf("设置文件里 = %q, want 只要 h265（禁用不落盘）", got)
	}

	// 关掉禁用，回到上次选的那套。
	if st, _ = a.SetActiveFilter("只要 h265", false); st.Off || st.Profile.Name != "只要 h265" {
		t.Fatalf("关掉禁用后没回到那套：%+v", st)
	}
}

// 删掉正在用的那套时，生效方案要跟着落到剩下里的第一套，而不是指向一个不存在的东西。
func TestDeleteActiveFilterFallsBack(t *testing.T) {
	defer store.SetDataDir(store.DataDir())
	store.SetDataDir(t.TempDir())

	a := &App{runner: engine.NewRunner()}
	a.settings = store.Settings{
		FilterProfiles: []store.FilterProfile{{Name: "默认"}, {Name: "只要 b"}},
		ActiveFilter:   "只要 b",
	}
	if err := store.SaveSettings(a.settings); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteFilterProfile("只要 b"); err != nil {
		t.Fatal(err)
	}
	if got := a.effectiveFilter().Name; got != "默认" {
		t.Errorf("生效方案 = %q, want 默认", got)
	}
	if got := store.LoadSettings().ActiveFilter; got != "默认" {
		t.Errorf("设置文件里 = %q, want 默认", got)
	}
}

// 列表多选走的就是这个出口（单删是"只勾了一行"的特例），所以这里量一次删一串。
//
// 两个方向都要钉住：只交「基本配置」那一个 id 时一个都不能少（它是所有模板的默认值来源，
// 整批失败才对），而它跟着另外几个一起交上来时**只跳过它自己**（用户勾的是另外几行，
// 让这一批全部失败等于"我勾了三个结果什么都没删"）。
func TestDeleteTemplatesSkipsGlobalInBatch(t *testing.T) {
	defer store.SetDataDir(store.DataDir())
	store.SetDataDir(t.TempDir())

	a := &App{runner: engine.NewRunner()}
	a.templates = []store.Template{
		{ID: store.GlobalTemplateID, Name: store.GlobalTemplateName, Global: true},
		{ID: "t-a", Name: "甲"},
		{ID: "t-b", Name: "乙"},
	}

	n, err := a.DeleteTemplates([]string{"t-a", "t-b"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("删掉 %d 个, want 2", n)
	}
	if got := len(a.templates); got != 1 || a.templates[0].ID != store.GlobalTemplateID {
		t.Errorf("剩下的 = %v, want 只有「基本配置」", a.templates)
	}

	// 只交全局那一个 id：一个都没删掉，而且必须**报错** —— 静默返回 0 会让界面报
	// "已删除 0 个模板"，读起来像是删成功了。
	if _, err := a.DeleteTemplates([]string{store.GlobalTemplateID}); err == nil {
		t.Error("只交全局那一套应该报错")
	}
}

func TestDeleteTemplatesKeepsGlobalAlongsideOthers(t *testing.T) {
	defer store.SetDataDir(store.DataDir())
	store.SetDataDir(t.TempDir())

	a := &App{runner: engine.NewRunner()}
	a.templates = []store.Template{
		{ID: store.GlobalTemplateID, Name: store.GlobalTemplateName, Global: true},
		{ID: "t-a", Name: "甲"},
		{ID: "t-b", Name: "乙"},
	}

	n, err := a.DeleteTemplates([]string{store.GlobalTemplateID, "t-a", "t-b"})
	if err != nil {
		t.Fatalf("带着全局一起交不该整批失败: %v", err)
	}
	if n != 2 {
		t.Errorf("删掉 %d 个, want 2（全局跳过，不算）", n)
	}
	if len(a.templates) != 1 || a.templates[0].ID != store.GlobalTemplateID {
		t.Errorf("剩下的 = %v, want 只有「基本配置」", a.templates)
	}
}
