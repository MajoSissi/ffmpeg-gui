package store

import (
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// DirSpec — 每一段产物都能自己决定"放到哪"
// ---------------------------------------------------------------------------

// Output destination modes, one per choice in 「输出方式」.
//
// The empty string means the same thing as OutputSame: a section whose mode was
// never set writes next to the source file. That is the plainest answer there
// is, and the only one that cannot lose a file.
const (
	OutputSame = "same" // 与源文件同目录
	// OutputSibling 锚在**添加的那个目录**上：添加 D:/V 就只在 D:/ 旁边建一个
	// 目录，整棵树的产物汇到那一个里去，形状靠 KeepTree 恢复。
	OutputSibling = "sibling"
	// OutputSiblingTop 锚在**文件自己所在的目录**上：添加 D:/V、文件在 D:/V/1，
	// 就在 D:/V/1 旁边建一个目录，于是每个子目录各出一个自己的。
	OutputSiblingTop = "siblingTop"
	OutputCustom     = "custom" // 指定的目录
)

// DefaultOutputSuffix is what OutputSibling appends when the user left both the
// prefix and the suffix blank.
//
// 同级目录 with nothing added would name the file's own folder, so the 产物 would
// land next to — and collide with — the input, which is the one thing a
// destination rule must not do. One underscore is enough to keep them apart.
const DefaultOutputSuffix = "_out"

// DirSpec says where one stage writes its files. The main output, the files
// rejected by the filter rules, the sources this template has already processed
// and the problem files (failed / warning) all use it, so every stage answers
// the same question the same way.
type DirSpec struct {
	Mode string `json:"mode"` // "" | same | sibling | siblingTop | custom
	// Dir is OutputCustom's directory, and what the user typed is what is used:
	// nothing is appended to it, so "C:/video" is C:\video and not
	// C:\video\<something>.
	Dir string `json:"dir"`
	// Prefix and Suffix wrap the anchor directory's name in the two sibling modes.
	//
	// 目录名被钉在路径的末尾，所以给"这个文件夹"加前缀只能连名字一起重写：把父
	// 目录取出来，再把 前缀+名字+后缀 拼成一个新的同级目录名。两种同级只是锚不同
	// （添加目录 / 文件所在目录），拼名的办法完全一样。
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	// KeepTree rebuilds the source's sub-directories under the directory the mode
	// resolved to, so a folder dropped in whole comes back out with the shape it
	// went in.
	//
	// 它对「原目录」没有意义（结构本来就在那里），对「同级顶层目录」也没有意义
	// （锚就是文件自己的目录，相对自己是空路径）—— 前端据此把开关灰掉
	// （`dirSpecHasRoom`）。
	KeepTree bool `json:"keepTree"`
}

// IsZero reports whether nothing at all was configured. It is the "this key
// predates the field" test in the legacy loader, nothing else.
func (d DirSpec) IsZero() bool {
	return d.Mode == "" && strings.TrimSpace(d.Dir) == "" &&
		strings.TrimSpace(d.Prefix) == "" && strings.TrimSpace(d.Suffix) == "" && !d.KeepTree
}

// ---------------------------------------------------------------------------
// 解析
// ---------------------------------------------------------------------------

// DestRequest asks where a file should land.
type DestRequest struct {
	Spec    DirSpec
	SrcPath string
	// SrcRoot is the directory the user added (scanned). It is what KeepTree is
	// measured against: the sub-tree that gets rebuilt is the one below *this*,
	// so a folder dropped in whole comes back out with the shape it went in.
	SrcRoot string
}

// ResolveDestDir returns the directory a file should be written to.
//
// It cannot fail. Every mode has an answer, and a section the user has not filled
// in yet degrades to "next to the source" rather than to a job that never runs.
func ResolveDestDir(req DestRequest) string {
	srcDir := filepath.Dir(req.SrcPath)

	switch req.Spec.Mode {
	case OutputSibling:
		// 锚在**添加的那个目录**：添加 D:/V（下面有 1、2 … 若干子目录），就只在
		// D:/ 旁边建一个产物目录，全部子目录的产物汇到同一个地方去。没有添加
		// 目录时（单个拖进来的文件）退回源文件所在目录（anchorDir），不然会算成
		// 一条相对路径，落在进程旁边而不是文件旁边。
		anchor := anchorDir(req.SrcRoot, srcDir)
		return under(
			filepath.Join(filepath.Dir(anchor), siblingName(req.Spec, anchor)),
			req.SrcRoot, srcDir, req.Spec.KeepTree)
	case OutputSiblingTop:
		// 锚在**文件自己所在的目录**：添加 D:/V、文件在 D:/V/1，配上前缀后缀就
		// 落在 D:/V/prefix_1_suffix —— 只看文件具体在哪，于是每个子目录各出一个
		// 自己的产物目录。
		//
		// KeepTree 在这里是空的：锚就是文件自己的目录，相对自己是空路径。前端
		// 也据此把这个开关灰掉。
		return filepath.Join(filepath.Dir(srcDir), siblingName(req.Spec, srcDir))
	case OutputCustom:
		root := cleanDir(req.Spec.Dir)
		if root == "" {
			return srcDir
		}
		return under(root, req.SrcRoot, srcDir, req.Spec.KeepTree)
	default: // "", OutputSame
		return srcDir
	}
}

// anchorDir is the folder 同级目录 measures from: the one the user added, or the
// file's own folder when there is no added root to speak of.
func anchorDir(srcRoot, srcDir string) string {
	if strings.TrimSpace(srcRoot) == "" {
		return srcDir
	}
	return srcRoot
}

// siblingName is the folder name OutputSibling creates beside the anchor folder.
//
// 两边都留空时补默认后缀：那正是拼出来等于锚目录名的唯一一种输入。
func siblingName(spec DirSpec, anchor string) string {
	prefix := strings.TrimSpace(spec.Prefix)
	suffix := strings.TrimSpace(spec.Suffix)
	if prefix == "" && suffix == "" {
		suffix = DefaultOutputSuffix
	}
	return prefix + leafName(anchor) + suffix
}

// under puts the source's own sub-directories back beneath root. Both sibling
// modes and OutputCustom rebuild the tree that way: their anchor sits above the
// file's own folder, so that folder's path *is* a sub-tree worth putting back.
// OutputSame does not need it — the structure is already where it belongs.
func under(root, srcRoot, srcDir string, keepTree bool) string {
	if !keepTree {
		return root
	}
	sub := subPath(srcRoot, srcDir)
	if sub == "" {
		return root
	}
	return filepath.Join(root, sub)
}

// subPath is srcDir relative to the added root: the extra directories
// 「保留目录结构」 puts back under whatever the mode resolved to.
func subPath(root, srcDir string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	rel, err := filepath.Rel(root, srcDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

// leafName is the last segment of a directory path.
//
// 不能直接用 filepath.Base：盘根的 Base 在 Windows 上是 `\` —— 去掉分隔符之后卷名也被
// 当成"前面的部分"吃掉了，于是剩下一个分隔符当名字。把分隔符写进表达式会散成一条自己
// 都不认识的路（随后还会被 Clean 折掉，只剩一个看着莫名其妙的结果）。先去掉尾部分隔符，
// 遇到这种只剩卷名的情况就用卷名本身（"E:\" → "E:"）：仍然难看，但至少是个名字。
func leafName(dir string) string {
	trimmed := strings.TrimRight(dir, `\/`)
	if trimmed == "" {
		return dir
	}
	base := filepath.Base(trimmed)
	if base == `\` || base == `/` || base == "." {
		return filepath.VolumeName(trimmed)
	}
	return base
}

// DirName is the 目录名 every condition is compared against.
//
// 和 `{dirName}` 走同一个函数：用户手上就一个名字，条件、变量、面板上显示的那一行
// 必须是同一个，否则"面板说命中、真跑没命中"又多一处能分家的地方。盘根的 `\` 不算
// 名字，退回盘符（`D:`）—— `filepath.Base` 会把它原样交回来。
func DirName(dir string) string { return leafName(dir) }

// cleanDir normalizes the separators a user may have typed ("C:/video") without
// turning "no directory at all" into the current directory, which is what
// filepath.Clean("") would hand back.
func cleanDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// Sanitize replaces the characters Windows refuses in a file or directory name.
//
// It is applied to values that come from the app rather than from the file
// system — a template name may well contain a colon — and never to a whole
// path, where that same colon is part of "C:".
func Sanitize(s string) string {
	if s == "" {
		return ""
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
