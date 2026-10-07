package engine

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"ffmpeggui/internal/store"
)

func newJobID() string { return store.ShortID() }

// SplitArgs splits a raw argument string into individual arguments, honouring
// single and double quotes so users can write values containing spaces.
func SplitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var inSingle, inDouble bool
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t' || r == '\r' || r == '\n') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// QuoteArg renders an argument the way it should be typed in a shell.
func QuoteArg(a string) string {
	if a == "" {
		return `""`
	}
	if !strings.ContainsAny(a, " \t\"'\\&|<>^()[]{};,=!%$#@`*?~") {
		return a
	}
	return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
}

// JoinArgs renders a full command line for display / history.
func JoinArgs(exe string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, QuoteArg(exe))
	for _, a := range args {
		parts = append(parts, QuoteArg(a))
	}
	return strings.Join(parts, " ")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// alignTo rounds v to the nearest multiple of n (at least n).
func alignTo(v, n int) int {
	if n <= 1 {
		return v
	}
	r := int(math.Round(float64(v)/float64(n))) * n
	if r < n {
		r = n
	}
	return r
}

// evenKeep makes sure a dimension is encodable by yuv420p.
func evenKeep(v int) int {
	if v < 2 {
		return 2
	}
	if v%2 != 0 {
		v--
	}
	return v
}

// ---------------------------------------------------------------------------
// Naming
// ---------------------------------------------------------------------------

// Naming carries the values available inside an output name pattern.
type Naming struct {
	Name     string // 源文件名（不含扩展名）
	Ext      string // 源扩展名（不含点）
	Template string
	Dir      string // 源目录名
	Index    int
}

// ExpandPattern replaces {token} placeholders in a rename pattern.
//
// The two callers mean different things by a file name, so they get different
// token sets rather than one compromise:
//
//   - ExpandOutputPattern is the 输出与命名 template. There is no {ext}: the name
//     is a file name, and the extension belongs to 「输出格式」. Offering it there
//     made the output depend on two settings at once, and the only symptom was a
//     preview that looked right until you changed container.
//   - ExpandPattern is for files being moved aside (被排除 / 已处理过的文件 /
//     错误 / 警告). Nothing re-wraps them, so they keep their own extension and
//     {ext} still means something.
func ExpandPattern(pattern string, n Naming) string {
	repl := map[string]string{
		"{name}":     n.Name,
		"{ext}":      n.Ext,
		"{template}": sanitize(n.Template),
		"{dir}":      sanitize(n.Dir),
		"{index}":    pad3(n.Index),
		"{idx}":      pad3(n.Index),
	}
	return expand(pattern, repl, false)
}

// ExpandOutputPattern is ExpandPattern without {ext}. A leftover token from an
// older template is stripped rather than left in: the extension that follows
// already comes from the container, so "clip.{ext}" would otherwise produce
// "clip..mp4".
func ExpandOutputPattern(pattern string, n Naming) string {
	repl := map[string]string{
		"{name}":     n.Name,
		"{template}": sanitize(n.Template),
		"{dir}":      sanitize(n.Dir),
		"{index}":    pad3(n.Index),
		"{idx}":      pad3(n.Index),
	}
	return expand(pattern, repl, true)
}

// expand substitutes the tokens it is given. stripExt removes a {ext} that the
// replacer left behind, which is how a template written before the token was
// dropped from 「输出与命名」 ends up as "clip" rather than "clip.{ext}".
func expand(pattern string, repl map[string]string, stripExt bool) string {
	out := pattern
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	if stripExt {
		out = strings.ReplaceAll(out, "{ext}", "")
	}
	return out
}

func pad3(v int) string {
	if v <= 0 {
		return ""
	}
	s := itoa(v)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// sanitize removes characters that are illegal in Windows file names.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	bad := `\/:*?"<>|`
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if strings.ContainsRune(bad, r) {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// ContainerExt normalizes a container name to a file extension.
func ContainerExt(container string) string {
	switch strings.ToLower(strings.TrimSpace(container)) {
	case "matroska":
		return "mkv"
	case "mp4", "mkv", "mov", "webm", "avi", "m4a", "mp3", "aac", "flac", "wav", "gif",
		"ts", "flv", "wmv", "ogv", "opus", "m4v", "mka", "ac3", "ogg":
		return strings.ToLower(container)
	case "quicktime":
		return "mov"
	case "mpegts":
		return "ts"
	case "asf":
		return "wmv"
	default:
		return strings.ToLower(strings.TrimLeft(container, "."))
	}
}

// ReplaceExt swaps the extension of name.
//
// It is right when the last dot really is an extension. It is wrong when it is
// not -- "qqq.123" has filepath.Ext ".123", and replacing that yields "qqq.mp4",
// quietly eating the version number. For an output name whose extension is
// decided by 「输出格式」, use EnsureExt instead.
func ReplaceExt(name, ext string) string {
	if ext == "" {
		return name
	}
	old := filepath.Ext(name)
	return strings.TrimSuffix(name, old) + "." + ext
}

// EnsureExt gives name the extension ext unless it already ends with it.
//
// The comparison is against the extension we intend to write, never against
// "does this name have any extension at all". filepath.Ext calls the ".123" in
// "qqq.123.mp4" an extension, so a source named "qqq.123.mp4" with the default
// {name} pattern produces the stem "qqq.123" -- and a naive Ext(name) == ""
// test then declares it complete. The file lands on disk as "qqq.123" with no
// extension, ffmpeg cannot infer a muxer from it, and the output is unplayable
// under its own name.
//
// The point of the naming pattern is the file NAME; the extension comes from
// 「输出格式」. So the only question worth asking is whether that extension is
// already there.
func EnsureExt(name, ext string) string {
	if ext == "" {
		return name
	}
	if strings.EqualFold(filepath.Ext(name), "."+ext) {
		return name
	}
	return name + "." + ext
}

// HumanSize renders a byte count the way a file manager would.
func HumanSize(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return itoa(int(n)) + " B"
	}
	return trimNum(v) + " " + units[i]
}

// HumanDuration renders seconds as h:mm:ss (or m:ss below an hour).
func HumanDuration(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	total := int(math.Round(sec))
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return itoa(h) + ":" + pad2(m) + ":" + pad2(s)
	}
	return itoa(m) + ":" + pad2(s)
}

func pad2(v int) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

func trimNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// EnsureUnique returns a path that does not exist yet by appending _1, _2, ...
func EnsureUnique(path string, exists func(string) bool) string {
	if !exists(path) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 1; i < 10000; i++ {
		cand := stem + "_" + itoa(i) + ext
		if !exists(cand) {
			return cand
		}
	}
	return path
}
