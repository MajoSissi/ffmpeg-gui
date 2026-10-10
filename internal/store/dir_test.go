package store

import (
	"path/filepath"
	"testing"
)

// dirRoot / dirDeep 是两条最常走的路：文件直挂在添加的目录下，和文件在子目录里。
//
// 写的是字面量而不是 filepath.Join("D:", "video")：后者在 Windows 上出来的是
// **盘相对路径** "D:video"，看着像绝对路径，filepath.Dir 对它只会给出 "D:."。
// 拿这种东西当基准，测的就不再是"同级目录"这件事了。
var (
	dirRoot = `D:\video`
	dirDeep = filepath.Join(dirRoot, "mmd", "sub")
)

// 三种输出方式各给一条路，逐条钉住落在哪。这个函数是"产物写到哪"的**唯一**出口，
// 它算错一次就是整批文件跑到别处去。
func TestResolveDestDirModes(t *testing.T) {
	src := filepath.Join(dirDeep, "a.mp4")
	cases := []struct {
		name string
		spec DirSpec
		want string
	}{
		{"留空 = 原目录", DirSpec{}, dirDeep},
		{"原目录", DirSpec{Mode: OutputSame}, dirDeep},
		{"原目录忽略保留结构（结构本来就在那）", DirSpec{Mode: OutputSame, KeepTree: true}, dirDeep},
		{"自定义目录", DirSpec{Mode: OutputCustom, Dir: `E:\out`}, `E:\out`},
		{"自定义目录 + 保留结构", DirSpec{Mode: OutputCustom, Dir: `E:\out`, KeepTree: true},
			filepath.Join(`E:\out`, "mmd", "sub")},
		{"自定义目录留空 = 原目录", DirSpec{Mode: OutputCustom}, dirDeep},
		// 同级是相对**添加的那个目录**（D:\video），不是相对文件所在的子目录：
		// 文件在 D:\video\mmd\sub，落点是 D:\video 的同级 D:\video_out。
		{"同级目录", DirSpec{Mode: OutputSibling, Suffix: "_out"}, filepath.Join(`D:\`, "video_out")},
		{"同级目录 + 保留目录结构", DirSpec{Mode: OutputSibling, Suffix: "_out", KeepTree: true},
			filepath.Join(`D:\`, "video_out", "mmd", "sub")},
		// 同级顶层目录量的是**文件自己所在的目录**：D:\video\mmd\sub 的同级是
		// D:\video\mmd，产物叫 sub_out。保留结构在这里没有意义（锚就是它自己）。
		{"同级顶层目录", DirSpec{Mode: OutputSiblingTop, Suffix: "_out"},
			filepath.Join(dirRoot, "mmd", "sub_out")},
		{"同级顶层目录忽略保留结构", DirSpec{Mode: OutputSiblingTop, Suffix: "_out", KeepTree: true},
			filepath.Join(dirRoot, "mmd", "sub_out")},
	}
	for _, tc := range cases {
		if got := ResolveDestDir(DestRequest{Spec: tc.spec, SrcPath: src, SrcRoot: dirRoot}); got != tc.want {
			t.Errorf("%s: %+v -> %q, want %q", tc.name, tc.spec, got, tc.want)
		}
	}
}

// 同级目录的名字 = 前缀 + 添加目录的名字 + 后缀。前缀是用户点名要的：目录名被钉在
// 路径的末尾，只有后缀的时候加不了前缀。
func TestResolveDestDirSiblingName(t *testing.T) {
	src := filepath.Join(dirDeep, "a.mp4")
	parent := filepath.Dir(dirRoot) // D:\
	cases := []struct {
		name string
		spec DirSpec
		want string
	}{
		{"只有后缀", DirSpec{Mode: OutputSibling, Suffix: "_out"}, filepath.Join(parent, "video_out")},
		{"只有前缀", DirSpec{Mode: OutputSibling, Prefix: "123_"}, filepath.Join(parent, "123_video")},
		{"前缀和后缀", DirSpec{Mode: OutputSibling, Prefix: "123_", Suffix: "_out"},
			filepath.Join(parent, "123_video_out")},
		{"另一端的前缀", DirSpec{Mode: OutputSibling, Prefix: "_"}, filepath.Join(parent, "_video")},
		// 两边都留空就会拼出一个等于添加目录的名字，产物会和输入挤在同一个文件夹里。
		// 补一个默认后缀是这里唯一能让它们分开的办法。
		{"两边都留空", DirSpec{Mode: OutputSibling}, filepath.Join(parent, "video_out")},
		{"空白字符不算填了", DirSpec{Mode: OutputSibling, Prefix: "  ", Suffix: " "},
			filepath.Join(parent, "video_out")},
	}
	for _, tc := range cases {
		if got := ResolveDestDir(DestRequest{Spec: tc.spec, SrcPath: src, SrcRoot: dirRoot}); got != tc.want {
			t.Errorf("%s: %+v -> %q, want %q", tc.name, tc.spec, got, tc.want)
		}
	}
}

// 「同级目录」量的是**添加的那个目录**：添加 D:/V（下面有 1、2 … 若干子目录），
// 只在 D:/ 的同级建一个 prefix_V_suffix，全部子目录的产物汇进去，
// 「保留目录结构」再把 1、2 原样搬回来 —— 而不是每个子目录旁边各出一个。
func TestResolveDestDirSiblingAnchorsOnAddedRoot(t *testing.T) {
	root := `D:\V`
	spec := DirSpec{Mode: OutputSibling, Prefix: "prefix_", Suffix: "_suffix"}
	for _, tc := range []struct{ sub, want string }{
		{"1", `D:\prefix_V_suffix`},
		{"2", `D:\prefix_V_suffix`},
		{"x\\y", `D:\prefix_V_suffix`},
		// 文件直接挂在添加的目录下时，子路径是空的，落点还是那一个目录。
		{"", `D:\prefix_V_suffix`},
	} {
		src := filepath.Join(root, tc.sub, "a.mp4")
		if got := ResolveDestDir(DestRequest{Spec: spec, SrcPath: src, SrcRoot: root}); got != tc.want {
			t.Errorf("文件在 %s -> %q, want %q", src, got, tc.want)
		}
	}
	// 同一棵树里每个文件的落点都一样，这是这一档存在的全部意义：
	// 添加目录只出**一个**产物目录，重跑时扫的仍然是原来那棵树。
	var first string
	for _, sub := range []string{"1", "2", "x", "x\\y"} {
		got := ResolveDestDir(DestRequest{
			Spec: spec, SrcPath: filepath.Join(root, sub, "a.mp4"), SrcRoot: root,
		})
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Errorf("文件在 %s -> %q，与 %q 不在同一个目录，说明锚不是添加目录", sub, got, first)
		}
	}
	if first != `D:\prefix_V_suffix` {
		t.Errorf("共同落点 = %q, want %q", first, `D:\prefix_V_suffix`)
	}
	// 加上「保留目录结构」之后子目录才重新分开，但顶层仍然只有那一个目录。
	kept := DirSpec{Mode: OutputSibling, Prefix: "prefix_", Suffix: "_suffix", KeepTree: true}
	if got, want := ResolveDestDir(DestRequest{
		Spec: kept, SrcPath: filepath.Join(root, "x", "y", "a.mp4"), SrcRoot: root,
	}), `D:\prefix_V_suffix\x\y`; got != want {
		t.Errorf("保留结构 -> %q, want %q", got, want)
	}
	// 没有添加目录时（单个拖进来的文件）退回它自己所在的目录，不然会算成一条
	// 相对路径，落在进程旁边而不是文件旁边。
	if got, want := ResolveDestDir(DestRequest{
		Spec: spec, SrcPath: filepath.Join(root, "1", "a.mp4"), SrcRoot: "",
	}), filepath.Join(root, "prefix_1_suffix"); got != want {
		t.Errorf("没有添加目录 -> %q, want %q", got, want)
	}
}

// 「同级顶层目录」量的是**文件自己所在的目录**，所以同一个添加目录下的每个子目录
// 各出一个自己的产物目录：添加 D:/V、文件在 D:/V/1，配 prefix_ / _suffix 落在
// D:/V/prefix_1_suffix —— 用户第 40 批的原话是"也就是只看文件具体所在的目录"。
//
// 与上面那档的区别只有锚，但差别是实质的：一棵子目录不一的树会得到 N 个产物目录
// 而不是 1 个。
func TestResolveDestDirSiblingTopPerSubFolder(t *testing.T) {
	root := `D:\V`
	spec := DirSpec{Mode: OutputSiblingTop, Prefix: "prefix_", Suffix: "_suffix"}
	for _, tc := range []struct{ sub, want string }{
		{"1", `D:\V\prefix_1_suffix`},
		{"2", `D:\V\prefix_2_suffix`},
		{"x\\y", `D:\V\x\prefix_y_suffix`},
		// 文件直接挂在添加的目录下时，它就是"顶层"，落点回到添加目录的同级。
		{"", `D:\prefix_V_suffix`},
	} {
		src := filepath.Join(root, tc.sub, "a.mp4")
		if got := ResolveDestDir(DestRequest{Spec: spec, SrcPath: src, SrcRoot: root}); got != tc.want {
			t.Errorf("文件在 %s -> %q, want %q", src, got, tc.want)
		}
	}
	// 「保留目录结构」在这里加不出任何东西：锚就是文件自己的目录，相对自己是空路径。
	// 开着它和关着它必须落在同一个地方 —— 灰掉那个开关的依据。
	kept := spec
	kept.KeepTree = true
	for _, sub := range []string{"1", "2", "x\\y", ""} {
		src := filepath.Join(root, sub, "a.mp4")
		with := ResolveDestDir(DestRequest{Spec: spec, SrcPath: src, SrcRoot: root})
		wo := ResolveDestDir(DestRequest{Spec: kept, SrcPath: src, SrcRoot: root})
		if with != wo {
			t.Errorf("文件在 %s: 保留结构把落点从 %q 变成了 %q，这里它不该有作用", sub, with, wo)
		}
	}
	// 这一档不需要「添加目录」也能工作：锚是文件自己算出来的，没有添加目录时
	// 「同级目录」和它落在同一个地方。
	if got, want := ResolveDestDir(DestRequest{
		Spec: spec, SrcPath: filepath.Join(root, "1", "a.mp4"), SrcRoot: "",
	}), `D:\V\prefix_1_suffix`; got != want {
		t.Errorf("没有添加目录 -> %q, want %q", got, want)
	}
}

// 留空 = 源文件所在目录。这是用户给的唯一一条无条件规则，任何方式都不该绕过它。
func TestResolveDestDirBlankIsSourceDir(t *testing.T) {
	src := filepath.Join(dirDeep, "a.mp4")
	for _, spec := range []DirSpec{
		{},
		{KeepTree: true}, // 结构已经在那里了，再补一次就重复
		{Mode: OutputCustom, Dir: "   "},
	} {
		if got := ResolveDestDir(DestRequest{Spec: spec, SrcPath: src, SrcRoot: dirRoot}); got != dirDeep {
			t.Errorf("%+v -> %q, want %q", spec, got, dirDeep)
		}
	}
}

// 「保留目录结构」补的是源文件相对「添加目录」的那一段，不是相对源目录：
// 补相对源目录的东西进去，等于把同一个名字拼两遍。
func TestResolveDestDirKeepTree(t *testing.T) {
	src := filepath.Join(dirDeep, "a.mp4")
	if got, want := ResolveDestDir(DestRequest{
		Spec: DirSpec{Mode: OutputCustom, Dir: `E:\out`, KeepTree: true}, SrcPath: src, SrcRoot: dirRoot,
	}), filepath.Join(`E:\out`, "mmd", "sub"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// 文件直挂在添加目录下时没有子结构可留，结果必须还是那个目录本身，
	// 不能多出一层 "." 或者空段。
	if got, want := ResolveDestDir(DestRequest{
		Spec:    DirSpec{Mode: OutputCustom, Dir: `E:\out`, KeepTree: true},
		SrcPath: filepath.Join(dirRoot, "a.mp4"), SrcRoot: dirRoot,
	}), `E:\out`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// 添加目录不在文件的上一层（文件是另选进来的）时，相对路径取不出来，
	// 就不补 —— 补一段 "..\.." 出来才是真的把人送错地方。
	if got, want := ResolveDestDir(DestRequest{
		Spec:    DirSpec{Mode: OutputCustom, Dir: `E:\out`, KeepTree: true},
		SrcPath: `F:\other\a.mp4`, SrcRoot: dirRoot,
	}), `E:\out`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 「具体路径没用变量就该放哪里放哪里」：字面路径后面不许再挂任何东西，
// 连源目录名都不补。用户的 "C:/video" 就是 C:\video。
func TestResolveDestDirLiteralPathIsUsedAsIs(t *testing.T) {
	src := filepath.Join(dirDeep, "a.mp4")
	spec := func(dir string) DestRequest {
		return DestRequest{Spec: DirSpec{Mode: OutputCustom, Dir: dir}, SrcPath: src, SrcRoot: dirRoot}
	}
	if got, want := ResolveDestDir(spec("C:/video")), filepath.Clean("C:/video"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// 反斜杠写法是同一个地方，不是两层目录。
	if got := ResolveDestDir(spec(`C:\video\`)); got != filepath.Clean(`C:\video`) {
		t.Errorf("windows separators were not normalised: %q", got)
	}
}

// 目录名：盘根的 filepath.Base 是 `\`，把分隔符当目录名会散成一条自己都不认识的
// 路 —— 同级目录是从目录名拼出来的，这里错一次整个模式就废了。
func TestLeafName(t *testing.T) {
	if got := leafName(`E:\`); got != "E:" {
		t.Errorf("盘根的目录名 = %q, want %q", got, "E:")
	}
	if got := leafName(dirRoot); got != "video" {
		t.Errorf("普通目录的目录名 = %q, want %q", got, "video")
	}
	if got := leafName(dirRoot + `\`); got != "video" {
		t.Errorf("带尾分隔符的目录名 = %q, want %q", got, "video")
	}
}

func TestSanitize(t *testing.T) {
	if Sanitize("a:b*c?d") != "a_b_c_d" {
		t.Errorf("Sanitize left something the file system refuses: %q", Sanitize("a:b*c?d"))
	}
	if Sanitize("") != "" {
		t.Error("Sanitize turned an empty string into something")
	}
}
