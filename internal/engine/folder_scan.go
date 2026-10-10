package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ffmpeggui/internal/store"
)

// ---------------------------------------------------------------------------
// 按目录名挑选要加入队列的目录
// ---------------------------------------------------------------------------
//
// 一个常见的情形：把一整棵树拖进来之后，真正想处理的是其中名字像某一批的那几个
// 文件夹。用户原话是"我想增加一个过滤包含指定名称的目录包括子目录，支持通配符,
// 支持多次过滤：比如先过滤 ffmpeg__* 之后我再过滤包含 h265"。后来又要反过来用：
// 整个文件夹照收，但不要 node_modules / cache 这种目录。
//
// 这个文件只做**有 IO 的那一半**：走目录、数文件、给预览一份结果。条件本身的形状
// 与匹配规则在 `store`（`DirFilter` / `MatchName` / `TakeName`）—— 因为同一组条件
// 还要被设置文件保存，而条件是常驻的：设一次，「添加文件夹」和拖入文件夹时都自动
// 套用。
//
// 方向（收 / 排）与"目录里收哪些文件"是同一条规则的两个面，所以它们都在这里：
// include 方向下命中的目录被当成一个整体收进来，exclude 方向下命中的目录连整棵
// 子树一起跳过、其余照收（此时落点是用户添加的那个文件夹本身）。

// FolderScan is one "add this folder, but only these sub-directories / these files"
// request.
//
// 它同时也是「添加文件夹」真正跑起来时用的那份请求（当前生效方案的规则提供条件，
// 目录来自用户选的那一个），所以预览与真加入走的是同一段代码。
//
// 两组规则分开写而不是摊平成六个字段：`Dirs` 和 `Files` 里都是 `[]NameFilter`，摊平
// 之后把文件条件传进目录条件是**同一个类型**，编译器不会拦，也没有第二个人看得出来
// —— 那种错只能在"筛错了"的时候被用户发现。形状和 `store.FilterProfile` 一模一样，
// 一个词只在这里说一遍。
type FolderScan struct {
	Dir string `json:"dir"`
	// Dirs 是目录规则：收哪几个子目录、是收还是排、命中几条才算了。
	Dirs store.NameRules `json:"dirs"`
	// Files 是文件规则：命中的目录里再按文件名筛一遍。它和 `Dirs` 完全独立 ——
	// 一个文件夹里"往下收哪几个子目录"和"这些目录里收哪几个文件"是两个问题。
	Files store.NameRules `json:"files"`
	// Recursive is 包含子目录: whether the *search* descends, and with it whether a
	// matched folder is taken whole.
	//
	// 它已经是**算完的**结果（`store.DirFilter.Recursive` 把设置和调用方两个来源
	// 收窄到一起），所以这里不再看 `Dirs`。
	Recursive bool `json:"recursive"`
}

// DirMatch is one selected directory and how many files it contributes.
type DirMatch struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
	// Rel is the path relative to the directory that was chosen, and it is what the
	// panel shows.
	//
	// 只写目录名是不够的：一棵树里每个批次都有一个 h265 子目录时，列表里会出现两行
	// 一模一样的 "h265"（各自带着不同的文件数），看不出要加的是哪一个。相对路径把
	// 它们分开，同时仍然短得能一眼扫完 —— 用户是在这个目录下面挑东西，绝对路径的
	// 前半段每一行都一样。
	Rel string `json:"rel"`
	// Files counts only the files this directory is the first to claim, so the
	// per-row numbers add up to TotalFiles instead of double-counting nested
	// matches. 排除方向下它数的是"这个目录会带走多少个文件"。
	Files int `json:"files"`
}

// relativeTo is the display path of one match under the chosen root.
//
// 根目录自己（没填条件那一档）没有相对路径可写，退回目录名；`filepath.Rel` 够不着的
// 情况同理 —— 列表里宁可短一个前缀，也不要出现 `..\..` 这种东西。
func relativeTo(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return filepath.Base(dir)
	}
	return rel
}

// FolderPreview is what the panel shows before anything is queued.
type FolderPreview struct {
	Root string `json:"root"`
	// Filtering is false when no condition is filled in, which the panel says in
	// words rather than by printing an empty list.
	Filtering bool `json:"filtering"`
	// Exclude mirrors FolderScan.Dirs.Exclude so the panel can phrase Dirs correctly:
	// 收的方向下列的是"会收进来的"，排的方向下列的是"会被跳过的"，同一份列表
	// 说的是两件相反的事。
	Exclude bool       `json:"exclude"`
	Dirs    []DirMatch `json:"dirs"`
	// DirsTotal is how many directories the conditions picked out, which is not
	// always len(Dirs): the list is capped (see maxPreviewDirs) because this reply
	// is fetched on every keystroke.
	DirsTotal int `json:"dirsTotal"`
	// TotalFiles counts every file that would be queued, once each.
	TotalFiles int `json:"totalFiles"`
	// FilteredFiles counts the media files the **file rules** dropped.
	//
	// 没有它的话，"规则写着排除，文件数却没变"和"规则根本没生效"在页面上长得一模
	// 一样 —— 而这个数字把两件事分开：它大于零就说明文件规则确实在筛，只是筛掉的
	// 都没进列表。
	FilteredFiles int `json:"filteredFiles"`
	// Scanned is how many directories were looked at. It is what turns "没有匹配"
	// from a silent zero into a fact: the search ran, over this many candidates.
	Scanned int `json:"scanned"`
}

// maxPreviewDirs caps the directory list a preview returns.
//
// 面板一屏最多画 200 行，而这个接口是**跟着打字跑的**（每改一个字防抖 180ms 一次）。
// 一条宽条件压在一棵大树上能匹配出几万个目录：真正会进队列的文件数照数不误，但没有
// 理由把几万条路径每次都序列化一遍、过一趟 WebView2 的消息通道 —— 那既是最慢的一段，
// 也是这条链路里唯一会因为"选了个大文件夹"而变重的东西。
const maxPreviewDirs = 200

// dirWalk is one directory traversal's outcome.
type dirWalk struct {
	Files []string
	// Pruned are the directories an exclude direction skipped, whole subtree
	// included. Only used to explain the result ("为什么一个文件都没有" /
	// "哪些被跳过了"), never to build the queue.
	Pruned []string
	// Filtered counts the media files the file rules dropped. 它和 Pruned 是两件事：
	// Pruned 是"整棵子树没进去"，这里是"文件本身被名字挡下了"。
	Filtered int
	// Examined counts the candidate directories the walk looked at.
	Examined int
	Errs     []string
}

// skipDirName is the "not worth walking" rule, shared by every walk here so that
// the preview and the real add can never disagree about which subtrees exist.
//
// 目录联接 / 符号链接不用在这里单独判：Go 在 Windows 上把它们报成**非目录**
// （`IsDir()` 为假，mode 是 `ModeSymlink` 或 `ModeIrregular`），所以
// `filepath.WalkDir` 本来就进不去，也就绕不成圈。这一条是实测过的，别照抄
// "遍历要跳过 reparse point"的通用建议再加一层永远不触发的判断。
func skipDirName(name string) bool { return strings.HasPrefix(name, ".") }

// ExpandFolder walks scan.Dir and returns the directories whose name matches,
// along with how many candidates were examined.
//
// 递归与否决定的是"搜到多深"，返回的始终是**目录**：命中的目录就是落点，
// 它下面收哪些文件由 mediaUnder 回答（同一个开关再说一遍同一个意思）。
//
// **用户添加的那个目录自己也是一个候选**，而且排在第一个。它是用户亲手挑的目录，
// 名字命中了就是要它：含子目录时连它下面的内容一起，不往下收时只收它这一层。把它
// 排除在外的话，"条件 包含 1 + 挑 D:\123"会一个文件都收不到 —— 而那个文件夹明明就
// 叫 123，用户看着自己的条件怎么看怎么对得上，却只能得到"没有子目录符合过滤条件"
// 这句反话。**只有收的方向有这一条**：排除方向下落点就是整个文件夹，命中的是它的
// 子树（见下面的 Exclude 分支）。
func ExpandFolder(scan FolderScan) ([]string, int, []string) {
	root := strings.TrimSpace(scan.Dir)
	if root == "" {
		return nil, 0, []string{"没有选择目录"}
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, 0, []string{fmt.Sprintf("%s: %v", root, err)}
	}
	if !st.IsDir() {
		return nil, 0, []string{fmt.Sprintf("%s: 不是目录", root)}
	}
	// 一条条件都没填 = 整个目录，和这个功能出现之前一模一样。
	if !scan.Filtering() {
		return []string{root}, 0, nil
	}
	// 排除方向没有"挑出哪几个"这一步：整个文件夹都是落点，被排除的子目录在
	// **收集文件**的时候跳过（那是唯一能把它们连子树一起丢掉的地方 —— 这里的
	// 返回值是"用户添加的目录"，把落点切成碎片就同时把输出目录的锚也切碎了）。
	if scan.Dirs.Exclude {
		return []string{root}, 0, nil
	}

	var dirs, errs []string
	// 根自己先过一次条件。`scanned` 也要把它算上：它确实是被看过的一个候选。
	examined := 1
	if store.MatchNameAll(store.DirName(root), scan.Dirs.Filters, scan.Dirs.MatchAll) {
		dirs = append(dirs, root)
	}

	if !scan.Recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, 0, []string{fmt.Sprintf("%s: %v", root, err)}
		}
		for _, e := range entries {
			if !e.IsDir() || skipDirName(e.Name()) {
				continue
			}
			examined++
			if store.MatchNameAll(e.Name(), scan.Dirs.Filters, scan.Dirs.MatchAll) {
				dirs = append(dirs, filepath.Join(root, e.Name()))
			}
		}
		return dirs, examined, nil
	}

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		// 根在上面的那次判断里已经过完了，别再挑一遍（挑两遍会让它出现在
		// dirs 里两次，收起来的文件数就凭空翻倍）。
		if !d.IsDir() || path == root {
			return nil
		}
		if skipDirName(d.Name()) {
			return filepath.SkipDir
		}
		examined++
		if store.MatchNameAll(d.Name(), scan.Dirs.Filters, scan.Dirs.MatchAll) {
			dirs = append(dirs, path)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", root, walkErr))
	}
	return dirs, examined, errs
}

// mediaUnder lists the media files one selected directory contributes.
//
// 命中的目录是被当成**整体**收进来的，所以递归开关在这里仍然是那个意思：打开时
// 连它下面的子目录一起收，关掉时只收直接放在它里面的。
//
// 两组规则各管一头，**互不相干**：
//
//   - `dirs` 只在**排除**方向下有意义：名字命中的子目录连整棵子树一起不要，落点仍然
//     是 dir 本身。这是"整个文件夹减去几个"唯一能表达出来的地方；收的方向下调用方
//     传空的那一组（命中的目录已经挑完了，再筛一遍会把它自己的子目录又筛一次）。
//   - `files` 决定**文件**的名字算不算数，两个方向都算。目录全收、文件只要其中几个
//     是最常见的用法之一。
//
// 文件规则挂在这里而不是等 runner 收完再筛，是因为预览走的就是这段遍历：离开这里去
// 别处筛一次，页面上写的"收 N 个文件"和真加进来的数量就会分家。
func mediaUnder(dir string, recursive bool, dirs, files store.NameRules) dirWalk {
	var out dirWalk
	// `NameRules.Take` 一起看开关和条件，所以"这套规则到底有没有在筛"只有它一个答案
	// —— 传进来一个 Enabled 为假的组，就是"不过滤"，和传空组是同一件事。
	keepFile := func(name string) bool {
		if files.Take(name) {
			return true
		}
		out.Filtered++
		return false
	}
	if !recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			out.Errs = append(out.Errs, fmt.Sprintf("%s: %v", dir, err))
			return out
		}
		for _, e := range entries {
			if e.IsDir() || !isMediaFile(e.Name()) {
				continue
			}
			if keepFile(e.Name()) {
				out.Files = append(out.Files, filepath.Join(dir, e.Name()))
			}
		}
		return out
	}

	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			out.Errs = append(out.Errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() {
			// dir 本身是用户自己选的：即使它是个联接，他要的也是它指过去的内容。
			if path == dir {
				return nil
			}
			if skipDirName(d.Name()) {
				return filepath.SkipDir
			}
			out.Examined++
			if !dirs.Take(d.Name()) {
				out.Pruned = append(out.Pruned, path)
				return filepath.SkipDir
			}
			return nil
		}
		if isMediaFile(d.Name()) && keepFile(d.Name()) {
			out.Files = append(out.Files, path)
		}
		return nil
	})
	if walkErr != nil {
		out.Errs = append(out.Errs, fmt.Sprintf("%s: %v", dir, walkErr))
	}
	return out
}

// Filtering reports whether the directory rules should narrow anything at all.
//
// 只看目录那一组：文件规则不改变"要挑哪几个目录"，它是在挑完之后对文件再筛一遍。
func (s FolderScan) Filtering() bool { return s.Dirs.Active() }

// ScanFolder is the dry run behind the page: the same selection the add will make,
// plus how many files each directory brings.
//
// 和真的加入走同一段遍历（ExpandFolder / mediaUnder），所以页面上写的数量就是按下
// 「开始添加」之后会进队列的数量 —— 页面自己另算一套，两边的差就只能在"加进来了才
// 发现"的时候暴露。
func ScanFolder(scan FolderScan) (FolderPreview, []string) {
	dirs, examined, errs := ExpandFolder(scan)
	pv := FolderPreview{
		Root:      strings.TrimSpace(scan.Dir),
		Filtering: scan.Filtering(),
		Exclude:   scan.Dirs.Exclude,
		Dirs:      []DirMatch{},
		Scanned:   examined,
	}

	if scan.Dirs.Exclude {
		// 排除方向：落点是整个文件夹，条件是沿途跳掉的那些子树。列出来的因此是
		// **被跳过的目录**，而 TotalFiles 是真正会进队列的数量 —— 一个说"少了
		// 什么"，一个说"还剩多少"，两个都要有才对得上"整个文件夹减去这些"。
		var all []DirMatch
		for _, root := range dirs {
			w := mediaUnder(root, scan.Recursive, scan.Dirs, scan.Files)
			errs = append(errs, w.Errs...)
			pv.Scanned += w.Examined
			pv.FilteredFiles += w.Filtered
			for _, pruned := range w.Pruned {
				// 被跳掉的子树报的是"这里有多少个文件会跟着没" —— 所以**不套**文件
				// 规则：那些文件是被目录规则带走的，拿文件规则再筛一遍会把"丢掉多
				// 少"说小。
				inner := mediaUnder(pruned, scan.Recursive, store.NameRules{}, store.NameRules{})
				errs = append(errs, inner.Errs...)
				all = append(all, DirMatch{
					Dir:   pruned,
					Name:  filepath.Base(pruned),
					Rel:   relativeTo(pv.Root, pruned),
					Files: len(inner.Files),
				})
			}
			pv.TotalFiles += len(uniqFiles(w.Files))
		}
		pv.DirsTotal = len(all)
		pv.Dirs = capDirs(all)
		return pv, errs
	}

	seen := map[string]bool{}
	var all []DirMatch
	for _, dir := range dirs {
		w := mediaUnder(dir, scan.Recursive, store.NameRules{}, scan.Files)
		errs = append(errs, w.Errs...)
		pv.FilteredFiles += w.Filtered
		m := DirMatch{Dir: dir, Name: filepath.Base(dir), Rel: relativeTo(pv.Root, dir)}
		for _, f := range w.Files {
			key := strings.ToLower(f)
			if seen[key] {
				continue
			}
			seen[key] = true
			m.Files++
		}
		pv.TotalFiles += m.Files
		all = append(all, m)
	}
	pv.DirsTotal = len(all)
	pv.Dirs = capDirs(all)
	return pv, errs
}

// capDirs trims the display list to maxPreviewDirs. The count stays in DirsTotal,
// so "还有 N 个目录未列出" is exact no matter how the cap moves.
//
// 空的时候给空切片而不是 nil：它会原样过 JSON，`null` 到前端就是 `null.length`，
// 面板上的一次崩溃只需要一个"什么都没有"的文件夹。
func capDirs(all []DirMatch) []DirMatch {
	if len(all) > maxPreviewDirs {
		return all[:maxPreviewDirs]
	}
	if all == nil {
		return []DirMatch{}
	}
	return all
}

// uniqFiles drops repeats within one walk (a media file can only be listed once
// per walk, but the caller's arithmetic is easier to trust when it is explicit).
func uniqFiles(files []string) []string {
	if len(files) < 2 {
		return files
	}
	seen := make(map[string]bool, len(files))
	out := files[:0]
	for _, f := range files {
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
