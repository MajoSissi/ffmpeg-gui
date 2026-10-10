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

// MoveRequest asks the engine to relocate a file. It is used by the filter rules,
// by 「已处理过的文件」 and by the error/warning policies, all of which let the
// user pick the destination with the same directory expression.
type MoveRequest struct {
	Src     string
	SrcRoot string
	Dirs    store.DirSpec
	Pattern string // 支持 {name} {ext} {template} {dir} {index} 等占位符
	// Overwrite lets the destination replace an existing file instead of being
	// given a "_1" name.
	Overwrite bool
	Copy      bool // true = 复制而不是移动
	Template  string
	Index     int
}

// Relocate moves or copies a file to its destination and returns the new path.
//
// An empty path with a nil error means there was nothing to do: the destination
// resolved to the source itself. Callers use that to leave the job's output
// blank instead of reporting a move that never happened.
func Relocate(req MoveRequest) (string, error) {
	if strings.TrimSpace(req.Src) == "" {
		return "", fmt.Errorf("源文件为空")
	}
	dir := store.ResolveDestDir(store.DestRequest{
		Spec:    req.Dirs,
		SrcPath: req.Src,
		SrcRoot: req.SrcRoot,
	})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建目录 %s: %w", dir, err)
	}

	base := filepath.Base(req.Src)
	ext := strings.TrimPrefix(filepath.Ext(base), ".")
	name := strings.TrimSuffix(base, filepath.Ext(base))

	newName := req.Pattern
	if strings.TrimSpace(newName) == "" {
		// A relocated file keeps its own extension -- there is no container to
		// re-wrap it in, so the name has to carry the extension this time.
		newName = "{name}." + ext
	}
	newName = ExpandPattern(newName, Naming{
		Name:     name,
		Ext:      ext,
		Template: req.Template,
		Dir:      filepath.Base(filepath.Dir(req.Src)),
		Index:    req.Index,
	})
	// Guard against illegal characters that would break on Windows.
	newName = store.Sanitize(newName)
	// Same rule as ResolveOutput: the file keeps its own extension, and "does it
	// already end with .mp4" is the question -- not "does it have a dot in it".
	// A source named "qqq.123.mp4" renames to "qqq.123" with a {name} pattern,
	// and testing for any extension left the moved file with none.
	newName = EnsureExt(newName, ext)

	dest := filepath.Join(dir, newName)
	if samePath(dest, req.Src) {
		// Already where it should be. Asked before the uniqueness check, which
		// would otherwise read the file as a name clash with itself and call the
		// result a_1.mp4: the file would not have gone anywhere, but its name
		// would have changed.
		return "", nil
	}
	if !req.Overwrite {
		dest = EnsureUnique(dest, func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		})
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

// OutputRoot returns the directory ResolveOutput would write into, without
// naming a file.
func OutputRoot(req OutputRequest) (string, error) {
	if req.Info == nil {
		return "", fmt.Errorf("缺少媒体信息")
	}
	// The same destination rule every other stage gets; the blanks were already
	// filled in by store.Template.Effective.
	return store.ResolveDestDir(store.DestRequest{
		Spec:    req.Tpl.OutDirSpec,
		SrcPath: req.Info.Path,
		SrcRoot: req.SrcRoot,
	}), nil
}

// ResolveOutput returns the final output path.
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
	dir, err := OutputRoot(req)
	if err != nil {
		return "", err
	}

	base := filepath.Base(info.Path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	pattern := strings.TrimSpace(tpl.OutPattern)
	if pattern == "" {
		// Keep the source file name, and nothing else. The output directory already
		// differs from the source one, so decorating the name as well would only add
		// noise -- and with a blank 输出目录 (the file stays where it is) the name is
		// the only thing keeping the result apart from the input.
		//
		pattern = "{name}"
	}
	// The name is a file name: the extension comes from the container and is
	// appended below, which is why ExpandOutputPattern has no {ext}.
	newName := ExpandOutputPattern(pattern, Naming{
		Name:     name,
		Ext:      outExt,
		Template: req.Tpl.Name,
		Dir:      filepath.Base(srcDir),
		Index:    req.Index,
	})
	newName = store.Sanitize(newName)

	// A trailing dot is not an extension. It shows up when an older pattern's
	// {ext} was stripped ("{name}.{ext}" -> "clip."), and letting it through
	// produces "clip..mp4" or a name Windows refuses to create.
	if strings.TrimSuffix(newName, ".") != newName {
		newName = strings.TrimSuffix(newName, ".")
	}
	// The extension always comes from the container, so it is added unless the
	// pattern already spells out that exact one. Testing "has any extension"
	// instead was a bug: a source named "qqq.123.mp4" yields the stem "qqq.123",
	// whose filepath.Ext is ".123", so the test passed and the output was written
	// as "qqq.123" -- no extension at all.
	newName = EnsureExt(newName, outExt)
	if strings.TrimSpace(newName) == "" {
		newName = base
	}

	dest := filepath.Join(dir, newName)

	// Never silently overwrite the source file itself.
	if samePath(dest, info.Path) {
		dest = filepath.Join(dir, EnsureExt(name, outExt))
		if samePath(dest, info.Path) || outExt == info.Ext {
			dest = filepath.Join(dir, EnsureExt(name+"_out", outExt))
		}
	}

	// An occupied path is NOT resolved away here. Renaming it would hand ffmpeg a
	// fresh name and quietly double every batch; deciding what an occupied path
	// means is the runner's job (engine.Runner.handleProcessed), which is the only
	// place that can also see the source file and the section's own record.
	return dest, nil
}
