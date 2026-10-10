package store

import (
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// 按名字挑东西
// ---------------------------------------------------------------------------
//
// 这段规则住在这里，不是 engine：它既被设置文件保存（过滤方案是常驻设置），又被扫描
// 目录/文件时求值。两边各存一份形状，读出来的条件就未必是写进去的那个。
//
// 三条判据：
//
//  1. **比的是名字**（`filepath.Base`），不是路径、也不是扩展名。条件因此可以写得很
//     短，搬动整棵树的时候也不会因为盘符变了就失配。
//  2. **一条都不填 = 不筛选**，退回"整个目录照样加 / 所有文件照样收"，也就是这个
//     功能出现之前的行为。页面上那条空行绝不能变成"什么都选不上"。
//  3. **一律不区分大小写**：Windows 上 `H265` 和 `h265` 是同一个目录，在筛选器里
//     把它们变成两个东西只会让人以为没匹配上。

const (
	FilterPrefix   = "prefix"
	FilterSuffix   = "suffix"
	FilterContains = "contains"
	FilterGlob     = "glob"
)

// NameFilter is one condition on a name.
type NameFilter struct {
	Mode  string `json:"mode"`
	Value string `json:"value"`
}

// Active reports whether this condition has anything to match on.
func (f NameFilter) Active() bool { return strings.TrimSpace(f.Value) != "" }

// ActiveFilters drops the blank rows. Everything downstream reads this list, so
// "有没有在筛选"只有一个判据。
func ActiveFilters(filters []NameFilter) []NameFilter {
	out := make([]NameFilter, 0, len(filters))
	for _, f := range filters {
		if f.Active() {
			out = append(out, f)
		}
	}
	return out
}

// MatchName reports whether one name satisfies one condition.
//
// `prefix` / `suffix` / `contains` 的值里出现 `*` 或 `?` 时自动按通配符比 —— 用户
// 不必先判断"我这个算前缀还是算通配符"。
func MatchName(name string, f NameFilter) bool {
	value := strings.ToLower(strings.TrimSpace(f.Value))
	if value == "" {
		return false
	}
	name = strings.ToLower(name)

	switch f.Mode {
	case FilterPrefix:
		if hasWildcard(value) {
			return globMatch(value+"*", name)
		}
		return strings.HasPrefix(name, value)
	case FilterSuffix:
		if hasWildcard(value) {
			return globMatch("*"+value, name)
		}
		return strings.HasSuffix(name, value)
	case FilterGlob:
		return globMatch(value, name)
	default: // FilterContains, 以及将来认不出的模式
		if hasWildcard(value) {
			return globMatch("*"+value+"*", name)
		}
		return strings.Contains(name, value)
	}
}

// MatchNameAll applies the whole condition list. With no active condition the
// answer is yes -- see NameFilter.Active.
func MatchNameAll(name string, filters []NameFilter, matchAll bool) bool {
	active := ActiveFilters(filters)
	if len(active) == 0 {
		return true
	}
	if matchAll {
		for _, f := range active {
			if !MatchName(name, f) {
				return false
			}
		}
		return true
	}
	for _, f := range active {
		if MatchName(name, f) {
			return true
		}
	}
	return false
}

func hasWildcard(s string) bool { return strings.ContainsAny(s, "*?") }

// TakeName 是"这个名字要不要"的唯一定义处，包含方向。
//
// 条件与方向是两个独立的东西：`MatchNameAll` 只回答"这个名字算不算数"，要不要则
// 取决于方向。把两者合成一个函数是为了让 include / exclude 在**同一个地方**分叉
// —— 目录遍历、预览、真加入三条路都问这一句，就不会出现"预览说排除、真跑却收了"。
//
// 它比的是**名字**，所以目录名和文件名共用它：两者问的是同一个问题，差别只在"拿谁的
// 名字来比"和"命中了要怎样"（目录是收不收，文件是进不进队列）。
//
// 一条条件都不填时两个方向都是"要"：条件留着但清空的面板上不该把整个文件夹变成空的。
func TakeName(name string, filters []NameFilter, matchAll, exclude bool) bool {
	if len(ActiveFilters(filters)) == 0 {
		return true
	}
	matched := MatchNameAll(name, filters, matchAll)
	if exclude {
		return !matched
	}
	return matched
}

// globMatch is filepath.Match with a bad pattern read as "no match".
//
// 用户正在往框里打字，"[" 这种半截模式是常态；报错会让人以为是自己填错了格式，
// 而正确的话是"先按现在这个样子比，没匹配上"。
func globMatch(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}

// ---------------------------------------------------------------------------
// 常驻的过滤方案
// ---------------------------------------------------------------------------

// NameRules is one group of conditions on a name, plus how to use them.
//
// 一套方案里有**两组**：目录规则按目录名挑要收哪几个目录，文件规则按文件名挑要收哪
// 几个文件。两组互不相干 —— "往下收哪几个子目录"和"这些目录里收哪几个文件"是两个
// 问题，用同一份条件回答它们只会让两边都写不准。
type NameRules struct {
	Enabled  bool         `json:"enabled"`
	Filters  []NameFilter `json:"filters"`
	MatchAll bool         `json:"matchAll"`
	// Exclude 把条件反过来用：命中的名字不要。
	//
	// 两种方向问的是同一个问题——"这个名字算不算数"——所以条件本身、匹配规则、
	// 满足全部还是任意一条全都不变，只有拿结果做什么不同。另开一套"排除条件"
	// 会立刻长出第二份匹配代码，而两份匹配代码就是"页面上说的"和"真跑一遍会做的"
	// 分家的地方。
	Exclude bool `json:"exclude"`
}

// Active reports whether this group should actually narrow anything.
//
// Enabled 和"有非空条件"缺一不可：一条都不填时它在界面上仍然是开着的（用户刚按的
// 那个开关），但行为必须是"不过滤"。
func (r NameRules) Active() bool { return r.Enabled && len(ActiveFilters(r.Filters)) > 0 }

// Take reports whether one name passes this whole group.
//
// 组一级的判据只有这一个：`TakeName` 问的是"按这些条件要不要这个名字"，而"这一组
// 到底有没有在筛"是另一个问题 —— 开关关着时它没在筛，条件填得再满也一样。两个问题
// 都摆在这里，调用点就不必各自拼一遍 `if r.Active() && TakeName(...)`，那种拼法写
// 三遍就会有一遍忘了看开关。
//
// 传一个零值组进来 = 不过滤（`Active()` 为假），所以"收的方向下不套这组规则"只需
// 要传空的那个，不必另开一条代码路径。
func (r NameRules) Take(name string) bool {
	if !r.Active() {
		return true
	}
	return TakeName(name, r.Filters, r.MatchAll, r.Exclude)
}

// Normalize trims the conditions and repairs what cannot be matched on.
//
// 空行直接丢掉而不是留着：它们对 `MatchName` 没有任何意义，留着只会在下次打开页面
// 时凭空多出几行空的。认不出的模式退回 `contains` —— 那是这四种里唯一"宽"的一种，
// 选它比让整条条件静默失效安全。
//
// 开关和方向都不动：它们比条件本身更容易被"顺手重置"掉，而设置文件每次读出来都会过
// 一趟这里，清一次就等于方向永远回不去"排除"。
func (r *NameRules) Normalize() {
	out := make([]NameFilter, 0, len(r.Filters))
	for _, f := range r.Filters {
		if !f.Active() {
			continue
		}
		f.Value = strings.TrimSpace(f.Value)
		switch f.Mode {
		case FilterPrefix, FilterSuffix, FilterGlob, FilterContains:
		default:
			f.Mode = FilterContains
		}
		out = append(out, f)
	}
	// 空切片而不是 nil：它会原样进设置文件，`[]` 比 `null` 少一次"这是没设过还是
	// 被清空了"的猜测。
	r.Filters = out
}

// DirFilter is the directory half of a profile: which sub-directories an add takes,
// and how far down it goes.
//
// TopOnly 是「含子目录」的反面 —— 反过来命名是为了让**零值就等于当前行为**。
//
// 老设置文件里没有这个键（用户自己的那份就没有），JSON 会把它读成 false。要是字段
// 叫 Recursive，老用户一升级就变成"只收这一层"而他们什么都没改过：添加文件夹会静默
// 漏掉整棵子树，产物少了一大半还看不出原因。反过来写，缺键 = 含子目录。
type DirFilter struct {
	NameRules
	TopOnly bool `json:"topOnly"`
}

// Recursive answers how deep an add should go, given what the caller asked for.
//
// 两个来源都只能往"更窄"的方向收：调用方说不递归就不递归，设置说只看这一层就只看这
// 一层。规则住在这里而不是 `app.enqueue` 里，是因为前端 mock 要说同一句话 —— 页面上
// 那个开关写着什么，真的加进来就得是什么。
//
// **它不看 Enabled**：这个开关管的是"往下收多深"，过滤整个关掉时同样有效。跟着
// Enabled 走的话，就会出现"开关灰着但它的值一直在生效"这种谁也说不清的状态。
func (d DirFilter) Recursive(caller bool) bool { return caller && !d.TopOnly }

// FilterProfile 是一套命名的过滤方案：目录规则一组、文件规则一组。
//
// 用户原话："可以设计能多个模版，可以保留，方便切换过滤条件"、"可以支持是否过滤文件，
// 文件可以和文件夹单独不同的过滤规则"。所以方案里是**两组各自独立的规则**，各有开关、
// 方向和条件，而不是一份条件被两边共用。
//
// **名字就是标识，没有 id。** 方案不会被别的东西引用（不像模板要被每个任务记着），
// 所以"改名会让引用失效"这一整类问题在这里不存在，多一个 id 只是多一处要保持同步。
//
// Description 是**给人看的一句话**，和模板的说明同一个位置（右侧「基本信息」里）——
// 方案的规则全在下面的条件行里，而条件行是写给判据看的，不是写给人看的：列表上一排
// 「前缀 cache · 后缀 .tmp」看着像两套方案的差别其实只是顺序不同。它**不参与任何判断**。
type FilterProfile struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Dirs        DirFilter `json:"dirs"`
	Files       NameRules `json:"files"`
}

// Active reports whether this profile would narrow anything at all.
func (p FilterProfile) Active() bool { return p.Dirs.Active() || p.Files.Active() }

// Normalize trims the name, the description and both rule groups.
func (p *FilterProfile) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	p.Dirs.Normalize()
	p.Files.Normalize()
}

// DefaultFilterName is the name given to the one profile every settings file has.
const DefaultFilterName = "默认"

// normalizeProfiles trims the names, drops the unnamed, collapses repeats, and
// guarantees at least one profile.
//
// 名字就是这套方案的身份，所以重名会让下拉里出现两个分不清的选项、也让"存"不知道
// 该覆盖谁。保存路径本来就按名字覆盖，重复只可能来自手改过的设置文件 —— 这里替它
// 收口，且保留先出现的那个。空名字直接丢掉（不是留下一个叫 "" 的）：它在界面上是
// 一行没有任何字的选项。
//
// **一份都没有时补上一套空的**，而不是留着零套：任务页那个下拉要有东西可选，否则
// 新装上来看到的是一排没有内容的控件，无处可点。
func normalizeProfiles(in []FilterProfile) []FilterProfile {
	out := make([]FilterProfile, 0, len(in)+1)
	seen := map[string]bool{}
	for _, p := range in {
		p.Normalize()
		if p.Name == "" {
			continue
		}
		key := strings.ToLower(p.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		out = append(out, FilterProfile{Name: DefaultFilterName})
	}
	return out
}

// PickProfile finds a profile by name, falling back to the first one.
//
// 找不到（被删掉、被改名、手改过的设置文件）时给第一套而不是零值：名字指向一套不存在
// 的方案时，静默退成"不过滤"会让用户以为刚才那套还在生效。
func PickProfile(name string, list []FilterProfile) FilterProfile {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, p := range list {
		if strings.ToLower(p.Name) == key {
			return p
		}
	}
	if len(list) == 0 {
		return FilterProfile{}
	}
	return list[0]
}
