package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
)

// EvaluateFilters decides whether an input should be processed. The returned
// reason is a short Chinese explanation shown in the UI and stored in history.
func EvaluateFilters(info *media.Info, f store.FilterSpec) (bool, string) {
	if info == nil {
		return false, "无法读取文件信息"
	}
	if !f.Enabled() {
		return true, ""
	}

	sizeMB := float64(info.Size) / (1024 * 1024)
	if f.MinSizeMB > 0 && sizeMB < f.MinSizeMB {
		return false, fmt.Sprintf("体积 %s 小于下限 %s", trimNum(sizeMB)+" MB", trimNum(f.MinSizeMB)+" MB")
	}
	if f.MaxSizeMB > 0 && sizeMB > f.MaxSizeMB {
		return false, fmt.Sprintf("体积 %s 超过上限 %s", trimNum(sizeMB)+" MB", trimNum(f.MaxSizeMB)+" MB")
	}
	if f.MinDuration > 0 && info.Duration > 0 && info.Duration < f.MinDuration {
		return false, fmt.Sprintf("时长 %s 短于下限 %s", HumanDuration(info.Duration), HumanDuration(f.MinDuration))
	}
	if f.MaxDuration > 0 && info.Duration > 0 && info.Duration > f.MaxDuration {
		return false, fmt.Sprintf("时长 %s 超过上限 %s", HumanDuration(info.Duration), HumanDuration(f.MaxDuration))
	}
	if long := info.LongEdge(); long > 0 {
		if f.MinLongEdge > 0 && long < f.MinLongEdge {
			return false, fmt.Sprintf("长边 %d 小于下限 %d", long, f.MinLongEdge)
		}
		if f.MaxLongEdge > 0 && long > f.MaxLongEdge {
			return false, fmt.Sprintf("长边 %d 超过上限 %d", long, f.MaxLongEdge)
		}
	}
	ext := strings.ToLower(info.Ext)
	if len(f.IncludeExts) > 0 && !containsStr(f.IncludeExts, ext) {
		return false, fmt.Sprintf("扩展名 .%s 不在允许列表内", ext)
	}
	if len(f.ExcludeExts) > 0 && containsStr(f.ExcludeExts, ext) {
		return false, fmt.Sprintf("扩展名 .%s 在排除列表内", ext)
	}
	return true, ""
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), v) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Excluded / problem file handling
// ---------------------------------------------------------------------------

// MoveRequest asks the engine to relocate a file. It is used by the filter rules
// and by the error/warning policies, both of which let the user pick any of the
// four output modes via Dest.
type MoveRequest struct {
	Src     string
	SrcRoot string
	Dest    store.DestRule
	Pattern string // 支持 {name} {ext} 等占位符
	// Fallback backsstop a Dest rule that leaves Dir blank for the custom/mirror
	// modes; without it those modes would have nowhere to write.
	Fallback  store.DestRule
	Overwrite bool
	Copy      bool // true = 复制而不是移动
	Template  string
}

// Relocate moves or copies a file to its destination and returns the new path.
func Relocate(req MoveRequest) (string, error) {
	if strings.TrimSpace(req.Src) == "" {
		return "", fmt.Errorf("源文件为空")
	}
	rule := req.Dest.Inherit(req.Fallback)
	if err := rule.Validate("转移目标"); err != nil {
		return "", err
	}

	dir, err := store.ResolveDestDir(store.DestRequest{
		Rule:          rule,
		SrcPath:       req.Src,
		SrcRoot:       req.SrcRoot,
		DefaultSuffix: store.DefaultOutputSuffix,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("未指定目标目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建目录 %s: %w", dir, err)
	}

	base := filepath.Base(req.Src)
	ext := strings.TrimPrefix(filepath.Ext(base), ".")
	name := strings.TrimSuffix(base, filepath.Ext(base))

	newName := req.Pattern
	if strings.TrimSpace(newName) == "" {
		newName = "{name}.{ext}"
	}
	newName = ExpandPattern(newName, Naming{
		Name:     name,
		Ext:      ext,
		Template: req.Template,
		Dir:      filepath.Base(filepath.Dir(req.Src)),
	})
	// Guard against illegal characters that would break on Windows.
	newName = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, newName)
	if filepath.Ext(newName) == "" && ext != "" {
		newName += "." + ext
	}

	dest := filepath.Join(dir, newName)
	if !req.Overwrite {
		dest = EnsureUnique(dest, func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		})
	}
	if samePath(dest, req.Src) {
		return req.Src, nil
	}

	if req.Copy {
		if err := copyFile(req.Src, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	if err := os.Rename(req.Src, dest); err != nil {
		// Cross-device rename: fall back to copy + delete.
		if err2 := copyFile(req.Src, dest); err2 != nil {
			return "", fmt.Errorf("移动失败: %v / %v", err, err2)
		}
		if err3 := os.Remove(req.Src); err3 != nil {
			return dest, fmt.Errorf("已复制到 %s，但删除源文件失败: %v", dest, err3)
		}
	}
	return dest, nil
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("复制数据失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Output path resolution
// ---------------------------------------------------------------------------

// ResolveOutput computes the destination path for a job.
type OutputRequest struct {
	Info *media.Info
	// Tpl must already be merged with the global template (see
	// store.Template.Effective) -- this function reads the output rules straight
	// off it and has no access to the global defaults.
	Tpl     store.Template
	Index   int
	SrcRoot string
}

// ResolveOutput returns the final output path (conflict policy applied).
func ResolveOutput(req OutputRequest) (string, error) {
	info := req.Info
	tpl := req.Tpl
	if info == nil {
		return "", fmt.Errorf("缺少媒体信息")
	}

	outExt := ""
	if c := strings.TrimSpace(tpl.Container); c != "" {
		outExt = ContainerExt(c)
	} else if info.Ext != "" {
		outExt = ContainerExt(info.Ext)
	}

	srcDir := filepath.Dir(info.Path)
	// The same four modes every other stage gets; the blanks were already filled
	// in by store.Template.Effective.
	dir, err := store.ResolveDestDir(store.DestRequest{
		Rule: store.DestRule{
			Mode:   tpl.OutMode,
			Dir:    tpl.OutDir,
			Suffix: tpl.OutSuffix,
		},
		SrcPath:       info.Path,
		SrcRoot:       req.SrcRoot,
		DefaultSuffix: store.DefaultOutputSuffix,
	})
	if err != nil {
		return "", err
	}

	base := filepath.Base(info.Path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	pattern := strings.TrimSpace(tpl.OutPattern)
	if pattern == "" {
		// Keep the source file name. The output directory already differs from the
		// source one, so decorating the name as well would only add noise -- and
		// when the mode is "与源文件同目录" the name is the only thing keeping the
		// result apart from the input.
		pattern = "{name}.{ext}"
	}
	// A pattern that only changes the extension should not gain a suffix.
	// {ext} resolves to the *output* extension, not the source one: the name
	// belongs to the file being written, so picking a different container has to
	// change it too (mkv source -> mp4 output must not be named .mkv).
	newName := ExpandPattern(pattern, Naming{
		Name:     name,
		Ext:      outExt,
		Template: req.Tpl.Name,
		Dir:      filepath.Base(srcDir),
		Index:    req.Index,
	})
	newName = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, newName)

	if filepath.Ext(newName) == "" && outExt != "" {
		newName += "." + outExt
	}
	if strings.TrimSpace(newName) == "" {
		newName = base
	}

	dest := filepath.Join(dir, newName)

	// Never silently overwrite the source file itself.
	if samePath(dest, info.Path) {
		dest = filepath.Join(dir, ReplaceExt(name, outExt))
		if samePath(dest, info.Path) || outExt == info.Ext {
			dest = filepath.Join(dir, ReplaceExt(name+"_out", outExt))
		}
	}

	switch tpl.OutConflict {
	case store.ConflictRename:
		dest = EnsureUnique(dest, func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		})
	}
	return dest, nil
}
