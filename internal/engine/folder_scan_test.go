package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ffmpeggui/internal/store"
)

// dirTree builds a small tree and returns its root:
//
//	root/
//	  ffmpeg__a/            a1.mp4, notes.txt
//	    h265/               h1.mkv
//	  ffmpeg__b/            b1.mp4
//	  h265/                 x1.mp4, x2.mkv
//	  other/                o1.mp4
//	    deep/
//	      h265/             d1.mp4
func dirTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, payload string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(payload), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("ffmpeg__a", "a1.mp4"), "x")
	write(filepath.Join("ffmpeg__a", "notes.txt"), "x")
	write(filepath.Join("ffmpeg__a", "h265", "h1.mkv"), "x")
	write(filepath.Join("ffmpeg__b", "b1.mp4"), "x")
	write(filepath.Join("h265", "x1.mp4"), "x")
	write(filepath.Join("h265", "x2.mkv"), "x")
	write(filepath.Join("other", "o1.mp4"), "x")
	write(filepath.Join("other", "deep", "h265", "d1.mp4"), "x")
	// 点开头的目录要整段跳过，和扫描媒体文件时同一个判断。
	write(filepath.Join(".cache", "ffmpeg__c", "c1.mp4"), "x")
	return root
}

func names(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, filepath.Base(d))
	}
	return out
}

// 用户添加的那个目录**自己也是一个候选**。
//
// 这是用户报上来的那条：条件 包含 `1`、挑的文件夹叫 `D:\123`，一个文件都没收进来，
// 面板上写着"没有符合条件的目录"。根被排除在候选之外时，那个文件夹明明叫 123 也够
// 不着条件 —— 用户看着自己的条件怎么看怎么对得上，只能得到一句反话。
func TestExpandFolderMatchesTheChosenFolderItself(t *testing.T) {
	root := filepath.Join(t.TempDir(), "123")
	mk := func(rel string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.mp4")     // 直接放在所选目录里
	mk("b.mkv")     //
	mk("sub/x.mp4") // 子目录里的：含子目录时才够得着

	one := []store.NameFilter{{Mode: store.FilterContains, Value: "1"}}

	// 含子目录：根自己命中，它整棵都是落点（`sub` 不命中，不进列表）。
	dirs, examined, errs := ExpandFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: one}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if got := names(dirs); len(got) != 1 || got[0] != "123" {
		t.Fatalf("dirs = %v, want [123]", got)
	}
	if examined != 2 {
		t.Errorf("examined = %d, want 2（根 + sub）", examined)
	}
	pv, _ := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: one}})
	if pv.TotalFiles != 3 {
		t.Errorf("含子目录 totalFiles = %d, want 3", pv.TotalFiles)
	}

	// 只看这一层：根自己命中，收的就是直接放在它里面的那两个。
	dirs, _, _ = ExpandFolder(FolderScan{Dir: root, Recursive: false, Dirs: store.NameRules{Enabled: true, Filters: one}})
	if got := names(dirs); len(got) != 1 || got[0] != "123" {
		t.Fatalf("只看一层 dirs = %v, want [123]", got)
	}
	pv, _ = ScanFolder(FolderScan{Dir: root, Recursive: false, Dirs: store.NameRules{Enabled: true, Filters: one}})
	if pv.TotalFiles != 2 {
		t.Errorf("只看一层 totalFiles = %d, want 2", pv.TotalFiles)
	}

	// 根不命中时和以前一样：在直接子目录里挑。
	dirs, _, _ = ExpandFolder(FolderScan{Dir: root, Recursive: false, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "sub"}}}})
	if got := names(dirs); len(got) != 1 || got[0] != "sub" {
		t.Fatalf("根不命中时 dirs = %v, want [sub]", got)
	}

	// **排除方向不把根算进去**：那里的落点就是整个文件夹，命中的是它的子树。挑一个
	// 自己就叫 123 的目录、条件又写着 1，要是根也参与命中，整个文件夹会被"排除"成
	// 空的 —— 而用户是奔着它来的。
	pv, _ = ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: one, Exclude: true}})
	if pv.TotalFiles != 3 || pv.DirsTotal != 0 {
		t.Errorf("排除方向 totalFiles = %d（want 3）、dirsTotal = %d（want 0）：根不参与命中",
			pv.TotalFiles, pv.DirsTotal)
	}
}

func TestExpandFolderFindsSubdirectories(t *testing.T) {
	root := dirTree(t)

	// 只看 h265：一棵树里三个（顶层的、ffmpeg__a 里的、other/deep 里的）。
	dirs, examined, errs := ExpandFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "h265"}}, MatchAll: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	got := names(dirs)
	want := []string{"h265", "h265", "h265"}
	if len(got) != len(want) {
		t.Fatalf("找到 %d 个目录 %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个 = %q, want %q", i, got[i], want[i])
		}
	}
	if examined == 0 {
		t.Error("examined 必须报出看过多少个目录")
	}
	// 点开头的目录不参与。
	for _, d := range dirs {
		if filepath.Base(filepath.Dir(d)) == ".cache" {
			t.Errorf("%q 落在点开头的目录里", d)
		}
	}
}

// 用户的例子：先 ffmpeg__* 再包含 h265（满足全部），只有同时满足的那个留下来。
func TestExpandFolderNarrowsWithEveryCondition(t *testing.T) {
	root := dirTree(t)
	dirs, _, errs := ExpandFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}, {Mode: store.FilterContains, Value: "h265"}}, MatchAll: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if got := names(dirs); len(got) != 0 {
		t.Errorf("这一层没有同时满足两条的目录，却选出了 %v", got)
	}

	// 换成"满足任意"，ffmpeg__a / ffmpeg__b / 三个 h265 都该进来。
	dirs, _, _ = ExpandFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}, {Mode: store.FilterContains, Value: "h265"}}, MatchAll: false}})
	if len(dirs) != 5 {
		t.Errorf("满足任意选出 %d 个（%v），want 5", len(dirs), names(dirs))
	}
}

// 关掉「包含子目录」时只在这一个目录里找，命中的目录也只收它自己直接放的文件。
func TestExpandFolderNotRecursive(t *testing.T) {
	root := dirTree(t)
	dirs, _, errs := ExpandFolder(FolderScan{Dir: root, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if got := names(dirs); len(got) != 2 || got[0] != "ffmpeg__a" || got[1] != "ffmpeg__b" {
		t.Fatalf("dirs = %v, want [ffmpeg__a ffmpeg__b]", got)
	}

	pv, _ := ScanFolder(FolderScan{Dir: root, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true}})
	// 不递归：ffmpeg__a 只算 a1.mp4，里面的 h265/h1.mkv 不算进来。
	if pv.TotalFiles != 2 {
		t.Errorf("不递归 totalFiles = %d, want 2", pv.TotalFiles)
	}
}

// 命中的目录是整体收进来的：打开递归时它下面的文件（含更深一层）全都算数，而且
// 一个个文件只算一次 —— 嵌套命中的目录会把同一批文件报两遍，那是面板上最难解释的
// 一种错。
func TestScanFolderCountsEachFileOnce(t *testing.T) {
	root := dirTree(t)

	// ffmpeg__a 与它里面的 h265 同时命中（满足任意）。
	pv, errs := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__a"}, {Mode: store.FilterContains, Value: "h265"}}, MatchAll: false}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	// 命中：ffmpeg__a、ffmpeg__a/h265、h265、other/deep/h265。
	// 文件：a1.mp4、h265/h1.mkv、x1.mp4、x2.mkv、d1.mp4 = 5 个，各一次。
	if pv.TotalFiles != 5 {
		t.Errorf("totalFiles = %d, want 5（每个文件只算一次）", pv.TotalFiles)
	}
	sum := 0
	for _, d := range pv.Dirs {
		sum += d.Files
	}
	if sum != pv.TotalFiles {
		t.Errorf("逐行相加 %d != 总数 %d —— 面板上的数字加不成总数", sum, pv.TotalFiles)
	}
	// ffmpeg__a 先被走到，h1.mkv 归它；里面的 h265 那行于是是 0。
	for _, d := range pv.Dirs {
		if d.Name == "h265" && filepath.Dir(d.Dir) == filepath.Join(root, "ffmpeg__a") && d.Files != 0 {
			t.Errorf("嵌套命中的目录重复计了 %d 个文件", d.Files)
		}
	}
}

// 命中的目录在列表里要分得开。
//
// 这棵树里有三个 h265，用户的条件又正好是按名字挑的 —— 只写目录名的话，列表里会
// 并排列出两行一模一样的 "h265"（各自带着不同的文件数），而"要加的是哪一个"正是
// 这个面板唯一要回答的问题。
func TestScanFolderGivesEachMatchItsOwnPath(t *testing.T) {
	root := dirTree(t)
	pv, errs := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "h265"}}, MatchAll: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	// 三个都叫 h265，名字那一列必然重复。
	if len(pv.Dirs) != 3 {
		t.Fatalf("命中 %d 个目录, want 3（%+v）", len(pv.Dirs), pv.Dirs)
	}
	seen := map[string]bool{}
	for _, d := range pv.Dirs {
		if d.Name != "h265" {
			t.Errorf("Name = %q, want h265", d.Name)
		}
		if seen[d.Rel] {
			t.Errorf("Rel 撞车：%q 出现两次（%+v）", d.Rel, pv.Dirs)
		}
		seen[d.Rel] = true
		// 相对路径要能拼回原来那个绝对目录，否则列表写的和真要加的就不是一个地方。
		if got := filepath.Join(root, d.Rel); got != d.Dir {
			t.Errorf("root+%q = %q, want %q", d.Rel, got, d.Dir)
		}
	}

	// 没填条件时落点是根自己，它没有相对路径（"."），退回目录名。
	pv2, _ := ScanFolder(FolderScan{Dir: root, Recursive: true})
	if len(pv2.Dirs) != 1 {
		t.Fatalf("Dirs = %+v", pv2.Dirs)
	}
	if pv2.Dirs[0].Rel != filepath.Base(root) {
		t.Errorf("根目录自己的 Rel = %q, want %q", pv2.Dirs[0].Rel, filepath.Base(root))
	}
}

// 一条条件都不填 = 整个目录，也就是这个功能出现之前的行为：根目录自己就是落点，
// 下面的媒体文件全进来。
func TestScanFolderWithoutConditions(t *testing.T) {
	root := dirTree(t)
	pv, errs := ScanFolder(FolderScan{Dir: root, Recursive: true})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if pv.Filtering {
		t.Error("没填条件时 Filtering 应当是 false")
	}
	if len(pv.Dirs) != 1 || pv.Dirs[0].Dir != root {
		t.Fatalf("落点应当是根目录自己，得到 %+v", pv.Dirs)
	}
	// 7 个媒体文件（notes.txt 不算，.cache 里的不算）。
	if pv.TotalFiles != 7 {
		t.Errorf("totalFiles = %d, want 7", pv.TotalFiles)
	}
	// 只在框里打了空格也算没填。
	pv2, _ := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "   "}}}})
	if pv2.Filtering || pv2.TotalFiles != 7 {
		t.Errorf("空白条件应当等同于没填，得到 filtering=%v files=%d", pv2.Filtering, pv2.TotalFiles)
	}
}

func TestScanFolderReportsWhatItLookedAt(t *testing.T) {
	root := dirTree(t)
	pv, _ := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterContains, Value: "zzz"}}, MatchAll: true}})
	if len(pv.Dirs) != 0 || pv.TotalFiles != 0 {
		t.Fatalf("不该匹配任何东西：%+v", pv)
	}
	// Scanned 是"看过多少个目录"，它把"什么都没匹配"和"根本没搜"区分开。
	if pv.Scanned == 0 {
		t.Error("Scanned = 0 —— 面板没法说清是搜过了还是没搜")
	}
}

func TestExpandFolderRejectsWhatItCannotScan(t *testing.T) {
	if _, _, errs := ExpandFolder(FolderScan{Dir: "  "}); len(errs) == 0 {
		t.Error("空目录路径应当报错")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, errs := ExpandFolder(FolderScan{Dir: file}); len(errs) == 0 {
		t.Error("指向文件应当报错")
	}
}

// 命中的目录扮演"用户添加的那个目录"：job.SourceRoot 必须是它，不是整棵树的根。
//
// 「同级目录」量的是 SourceRoot，所以这一条错了，产物就会落到用户根本没选的那一层
// 旁边 —— 而且是那种"看着也挺合理"的错。
//
// 过滤条件现在是挂在 InputItem 上的（`App.enqueue` 从设置里读出来盖上去），所以这条
// 测试走的就是「添加文件夹」和拖入**真实**过的那段代码，而不是另开一个入口。
func TestAddInputsBindsSourceRootToTheMatchedFolder(t *testing.T) {
	root := dirTree(t)
	r := newDeleteRunner()

	n, errs := r.AddInputs([]InputItem{{Path: root, IsDir: true, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true}}}, "t1", "模板")
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if n != 3 {
		t.Fatalf("加入 %d 个任务，want 3（a1.mp4 / h1.mkv / b1.mp4）", n)
	}
	want := map[string]string{
		"a1.mp4": filepath.Join(root, "ffmpeg__a"),
		"h1.mkv": filepath.Join(root, "ffmpeg__a"),
		"b1.mp4": filepath.Join(root, "ffmpeg__b"),
	}
	for _, j := range r.Jobs() {
		if got := want[j.InputName]; got != j.SourceRoot {
			t.Errorf("%s 的 SourceRoot = %q, want %q", j.InputName, j.SourceRoot, got)
		}
		delete(want, j.InputName)
	}
	if len(want) != 0 {
		t.Errorf("这几个文件没进队列：%v", want)
	}
	// 编号从 1 开始连续，和拖入那条路径一样（{index} 读的是它）。
	for i, j := range r.Jobs() {
		if j.Index != i+1 {
			t.Errorf("第 %d 个任务的 Index = %d, want %d", i, j.Index, i+1)
		}
	}
}

// 同一个文件被两个命中的目录同时覆盖时只进队列一次 —— 去重和拖入走的是同一段。
func TestAddInputsDeduplicatesAcrossMatchedFolders(t *testing.T) {
	root := dirTree(t)
	r := newDeleteRunner()
	n, _ := r.AddInputs([]InputItem{{Path: root, IsDir: true, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{
		{Mode: store.FilterPrefix, Value: "ffmpeg__a"},
		{Mode: store.FilterContains, Value: "h265"},
	}, MatchAll: false}}}, "t1", "模板")
	// a1.mp4 / ffmpeg__a/h265/h1.mkv / h265/x1.mp4 / h265/x2.mkv / other/deep/h265/d1.mp4
	if n != 5 {
		t.Fatalf("加入 %d 个任务，want 5（每个文件一次）", n)
	}
	seen := map[string]bool{}
	for _, j := range r.Jobs() {
		key := strings.ToLower(j.Input)
		if seen[key] {
			t.Errorf("%q 进了两次", j.Input)
		}
		seen[key] = true
	}
}

// 一个都没命中要说出来。
//
// 静默地什么都不加是最难查的一种：文件夹明明"进去了"，列表却是空的，而界面上没有
// 任何地方提到过滤条件还在拦着。
func TestAddInputsSaysWhenNothingMatches(t *testing.T) {
	root := dirTree(t)
	r := newDeleteRunner()
	n, errs := r.AddInputs([]InputItem{{Path: root, IsDir: true, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "没这个目录"}}, MatchAll: true}}}, "t1", "模板")
	if n != 0 {
		t.Fatalf("加入 %d 个任务，want 0", n)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "没有子目录符合过滤条件") {
		t.Errorf("errs = %v, want 一条说明过滤条件的", errs)
	}
}

// ---------------------------------------------------------------------------
// 排除方向
// ---------------------------------------------------------------------------
//
// 同一个条件反过来用：命中的子目录不要（连它下面整棵子树一起），其余照收。它和收的
// 方向共用条件、共用匹配规则，只在"拿结果做什么"上分叉。

// 排除方向的落点始终是**用户添加的那个目录本身**。
//
// 落点不能换成"剩下的那几个目录"：`SourceRoot` 决定产物落在哪、`{dirPath}` 这类目录
// 表达式的根是哪一个，把它切碎之后输出目录的锚也跟着碎 —— 「同级目录」会写到用户根本
// 没选过的那一层旁边，而结果看着还挺像那么回事。
func TestExpandFolderExcludeKeepsTheChosenRoot(t *testing.T) {
	root := dirTree(t)
	dirs, _, errs := ExpandFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true, Exclude: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("排方向的落点应当是根自己，得到 %v", dirs)
	}
}

// 排除方向的预览列的是**被跳过的目录**，而 totalFiles 说的是"还剩多少"。
//
// 两个数字都要有才对得上"整个文件夹减去这些"：只报被跳过的，看不出还剩多少；只报剩
// 下的，看不出条件是不是真咬到了东西 —— 一条都没咬到时两边都不动，正着看也一样。
func TestScanFolderExcludeListsPrunedSubtrees(t *testing.T) {
	root := dirTree(t)
	pv, errs := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true, Exclude: true}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if !pv.Filtering || !pv.Exclude {
		t.Fatalf("filtering=%v exclude=%v, want true/true", pv.Filtering, pv.Exclude)
	}
	// 被跳过的：ffmpeg__a（连它里面的 h265）与 ffmpeg__b。FFmpeg__a 里那层 h265 不该
	// 再单列一行 —— 它整棵子树都已经跟着被丢掉了。
	if pv.DirsTotal != 2 || len(pv.Dirs) != 2 {
		t.Fatalf("被跳过的目录 %d 个（%+v），want 2", pv.DirsTotal, pv.Dirs)
	}
	got := map[string]int{}
	for _, d := range pv.Dirs {
		got[d.Name] = d.Files
	}
	// 每行数的是"这个目录会带走多少文件"，所以 ffmpeg__a 报的是 2（a1.mp4 + h265/h1.mkv），
	// 不是它自己那一层的 1。
	if got["ffmpeg__a"] != 2 || got["ffmpeg__b"] != 1 {
		t.Errorf("被带走的文件数 = %v, want ffmpeg__a:2 ffmpeg__b:1", got)
	}
	// 7 个媒体文件里被排除 3 个，剩下 4 个（x1/x2/o1/d1）。
	if pv.TotalFiles != 4 {
		t.Errorf("totalFiles = %d, want 4（7 个减去被排除的 3 个）", pv.TotalFiles)
	}
}

// 排除条件一条都没咬到时整个文件夹照收，而且 Dirs 不能是 null。
//
// 这是这个方向的默认状态，也是条件写歪时最需要看得出来的那一种。Dirs 走 JSON 到前端
// 就是 `null.length` —— 面板上的一次崩溃只需要一个"什么都没排除"的文件夹。
func TestScanFolderExcludeWithNoMatchTakesEverything(t *testing.T) {
	root := dirTree(t)
	pv, _ := ScanFolder(FolderScan{Dir: root, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "没这个目录"}}, MatchAll: true, Exclude: true}})
	if pv.DirsTotal != 0 || len(pv.Dirs) != 0 {
		t.Errorf("不该跳过任何目录：%+v", pv.Dirs)
	}
	if pv.TotalFiles != 7 {
		t.Errorf("totalFiles = %d, want 7（一个都没排除）", pv.TotalFiles)
	}
	if pv.Dirs == nil {
		t.Error("Dirs 是 nil —— 过 JSON 之后前端拿到的是 null.length")
	}
}

// 排除方向真的会跑：进队列的是"整个文件夹减去被跳过的子树"，而 SourceRoot 还是用户
// 添加的那个目录。
func TestAddInputsExcludeDirection(t *testing.T) {
	root := dirTree(t)
	r := newDeleteRunner()
	n, errs := r.AddInputs([]InputItem{{Path: root, IsDir: true, Recursive: true, Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterPrefix, Value: "ffmpeg__"}}, MatchAll: true, Exclude: true}}}, "t1", "模板")
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if n != 4 {
		t.Fatalf("加入 %d 个任务，want 4（x1 / x2 / o1 / d1）", n)
	}
	for _, j := range r.Jobs() {
		if j.SourceRoot != root {
			t.Errorf("%s 的 SourceRoot = %q, want %q（落点是用户添加的那个目录）",
				j.InputName, j.SourceRoot, root)
		}
		rel, err := filepath.Rel(root, j.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(rel, "ffmpeg__") {
			t.Errorf("%q 落在被排除的目录里", j.Input)
		}
	}
}

// 全被排除要说出来，而且说法和"没有目录符合条件"不一样。
//
// 一个是"没挑着要的"，一个是"剩下的一个都不剩"：用户要改的东西不同（前者放宽条件，
// 后者收紧或者干脆换个方向），同一句话会让其中一个永远查不出来。
func TestAddInputsSaysWhenEverythingIsExcluded(t *testing.T) {
	root := dirTree(t)
	r := newDeleteRunner()
	n, errs := r.AddInputs([]InputItem{{
		Path: root, IsDir: true, Recursive: true, // 每个目录名都符合 → 全都排除掉。
		Dirs: store.NameRules{Enabled: true, Filters: []store.NameFilter{{Mode: store.FilterGlob, Value: "*"}}, MatchAll: true, Exclude: true},
	}}, "t1", "模板")
	if n != 0 {
		t.Fatalf("加入 %d 个任务，want 0", n)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "全部文件都被过滤条件排除了") {
		t.Errorf("errs = %v, want 一条说明全被排除的", errs)
	}
}
