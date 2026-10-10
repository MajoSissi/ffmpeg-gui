package store

import (
	"strings"
	"testing"
)

// {token:N} 的扫描只在这一处：目录表达式和输出文件名称都是往 resolve 里塞一张不同
// 的表。所以这里钉的是"扫描"本身 —— 整段匹配、单趟、认不出就原样还回去。
func TestReplaceTokensScan(t *testing.T) {
	resolve := func(t TokenRef) (string, bool) {
		switch t.Name {
		case "n":
			return FormatIndex(7, t.Arg, t.HasArg), true
		case "a":
			return t.Plain("A")
		case "echo":
			return "{n}", true
		}
		return "", false
	}
	cases := []struct{ expr, want string }{
		{"", ""},
		{"{n}", "7"},
		{"{n:3}", "007"}, // 整段匹配，不是先把 {n} 换掉、把 ":3}" 留在后面
		{"{n}{n:2}", "707"},
		{"x{n}y", "x7y"},
		{"{a}", "A"},
		{"{a:2}", "{a:2}"}, // 名字类 token 不收参数：留着比猜一个好
		{"{b}", "{b}"},     // 不认识的 token
		{"{n:x}", "{n:x}"}, // 参数不是整数：半替换比不替换难查
		{"{n", "{n"},       // 没有右括号：剩下的原样吐回去
		{"{n:", "{n:"},
		{"中{n}文", "中7文"},
		{"{{n}}", "{{n}}"},
		// 单趟，替换出来的东西不会被再扫一遍。以前是逐个 token 全局替换，谁先谁后由
		// map 的遍历顺序决定 —— 值里正好含一个 token 的时候就会时对时错。
		{"{echo}", "{n}"},
	}
	for _, tc := range cases {
		if got := ReplaceTokens(tc.expr, resolve); got != tc.want {
			t.Errorf("ReplaceTokens(%q) = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

// 序号宽度：默认不补零，写了 :N 才补到 N 位。0 和 1 是"一位数的宽度"，和干脆不写
// 是一回事 —— 这就是 {index} 与 {index:0} / {index:1} 完全一样的原因。
func TestFormatIndexWidth(t *testing.T) {
	cases := []struct {
		idx, width int
		has        bool
		want       string
	}{
		{0, 0, false, ""}, // 这一批里这个文件没有序号
		{0, 3, true, ""},  // 补零不会把"没有"变成"000"
		{-1, 0, false, ""},
		{7, 0, false, "7"},
		{7, 0, true, "7"},
		{7, 1, true, "7"},
		{7, 2, true, "07"},
		{7, 3, true, "007"},
		{12, 3, true, "012"},
		{12345, 3, true, "12345"},                    // 宽度比数字短就照原样，不截断
		{4, 99, true, strings.Repeat("0", 31) + "4"}, // 宽度有上限
	}
	for _, tc := range cases {
		if got := FormatIndex(tc.idx, tc.width, tc.has); got != tc.want {
			t.Errorf("FormatIndex(%d, %d, %v) = %q, want %q", tc.idx, tc.width, tc.has, got, tc.want)
		}
	}
}
