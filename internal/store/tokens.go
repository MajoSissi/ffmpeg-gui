package store

import (
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// {token} 与 {token:N} —— 目录表达式和输出文件名称共用的一套扫描
// ---------------------------------------------------------------------------

// TokenRef is one {name} or {name:N} as it appears in a pattern.
type TokenRef struct {
	Name   string
	Arg    int
	HasArg bool
}

// Plain is for the tokens whose value is a name rather than a path. A name takes
// no argument: "which level" is not a question a name can answer, and a silently
// ignored `{dirName:-1}` would look like it worked. Reporting the token as
// unknown leaves it visible in the result instead.
func (t TokenRef) Plain(val string) (string, bool) {
	if t.HasArg {
		return "", false
	}
	return val, true
}

// ReplaceTokens substitutes every {name} / {name:N} that resolve knows.
//
// One left-to-right pass, not one pass per token: a token's value is never
// scanned again. Re-substituting used to depend on map iteration order, so a
// value that happened to contain a token expanded differently from run to run.
//
// Anything it cannot resolve — an unknown name, or an argument that is not an
// integer — is copied out unchanged, braces included. A `{name}` written in a
// directory expression is a mistake worth seeing: deleting it quietly turns
// "D:/v/{name}" into "D:/v", one directory holding everything the user meant to
// keep apart.
//
// resolve returns the replacement and whether it knows the token. ("", true) is
// how a token that should disappear says so.
func ReplaceTokens(expr string, resolve func(TokenRef) (string, bool)) string {
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if expr[i] != '{' {
			b.WriteByte(expr[i])
			i++
			continue
		}
		end := strings.IndexByte(expr[i:], '}')
		if end < 0 {
			b.WriteString(expr[i:])
			break
		}
		end += i
		ref, ok := parseToken(expr[i+1 : end])
		if !ok {
			b.WriteByte(expr[i])
			i++
			continue
		}
		val, known := resolve(ref)
		if !known {
			b.WriteString(expr[i : end+1])
			i = end + 1
			continue
		}
		b.WriteString(val)
		i = end + 1
	}
	return b.String()
}

// parseToken reads what sits between a pair of braces: "index", "index:2",
// "dirPath:-1". A non-numeric argument is not a token at all, so "{index:x}"
// stays whole instead of half-replacing.
func parseToken(inner string) (TokenRef, bool) {
	ref := TokenRef{Name: inner}
	if c := strings.IndexByte(inner, ':'); c >= 0 {
		n, err := strconv.Atoi(inner[c+1:])
		if err != nil {
			return TokenRef{}, false
		}
		ref = TokenRef{Name: inner[:c], Arg: n, HasArg: true}
	}
	if ref.Name == "" {
		return TokenRef{}, false
	}
	return ref, true
}

// maxIndexWidth caps the zero padding. A file name is not the place to find out
// that someone typed one digit too many, and the repeat below would happily
// allocate the megabytes a typo asks for.
const maxIndexWidth = 32

// FormatIndex renders a batch index for {index}.
//
// The width is what the user asked for: {index:3} is "007". Without one — or
// with 0 or 1, which are the widths of a number written plainly — the number is
// left as it is, so {index} is "7". An index of 0 or less means this file has no
// number in this batch, and it stays empty rather than becoming a literal "0" in
// a file name.
func FormatIndex(idx, width int, hasWidth bool) string {
	if idx <= 0 {
		return ""
	}
	s := strconv.Itoa(idx)
	if !hasWidth || width <= len(s) {
		return s
	}
	if width > maxIndexWidth {
		width = maxIndexWidth
	}
	return strings.Repeat("0", width-len(s)) + s
}
