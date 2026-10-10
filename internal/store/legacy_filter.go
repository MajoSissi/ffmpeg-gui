package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// 老文件里的过滤条件
// ---------------------------------------------------------------------------
//
// 第 45 批之前，设置文件里是两样东西：
//
//	dirFilter     一套"当前生效的目录条件"，没有名字
//	filterPresets 几套**备着的**命名条件，只有目录规则
//
// 现在只有一样：一套套命名的方案（`filterProfiles`），默认生效的那套由 `activeFilter`
// 指出来。多出来的这层名字不是装饰 —— 任务页上要有一个"多个过滤的选项"的下拉，而一个
// 没有名字的东西没法出现在下拉里。
//
// **为什么不能像别的旧键那样直接忽略**：dirFilter 是用户真正在用的那套条件（条件、
// 方向、"含子目录"全在里面，用户手上那份就有七条）。丢掉它，攒了很久的条件会一声不响
// 地消失，界面上只留下一套空规则 —— 用户看到的是"我的过滤怎么没了"，而不是"升级了"。
//
// 翻译结果不写回文件：下一次 SaveSettings 只写新键，老键自然消失。
// 这个文件可以整体删掉——等到没有人的 settings.json 里还有 dirFilter 或 filterPresets。

// legacyFilterPreset is the 第 45 批之前 shape of a saved profile: 只有目录规则。
type legacyFilterPreset struct {
	Name   string    `json:"name"`
	Filter DirFilter `json:"filter"`
}

// legacyFilterSettings holds only the keys this translation reads.
//
// DirFilter is a pointer on purpose: 只有真的写在文件里的那个键才算"设置过"。一个压根
// 没有这个键的文件是全新安装，不该凭空多出一套叫「默认」的方案来。
type legacyFilterSettings struct {
	DirFilter     *DirFilter           `json:"dirFilter"`
	FilterPresets []legacyFilterPreset `json:"filterPresets"`
}

// adoptLegacyFilters turns 第 45 批之前的两样东西 into one list of profiles.
func adoptLegacyFilters(s *Settings) {
	// 已经是新文件了（里面有方案），没什么可翻的：再拿老键去补只会凭空多出一套。
	if len(s.FilterProfiles) > 0 {
		return
	}
	b, err := os.ReadFile(SettingsPath())
	if err != nil {
		return
	}
	var old legacyFilterSettings
	if err := json.Unmarshal(b, &old); err != nil {
		return
	}
	if old.DirFilter == nil && len(old.FilterPresets) == 0 {
		return
	}

	taken := map[string]bool{}
	for _, p := range old.FilterPresets {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}

	out := make([]FilterProfile, 0, len(old.FilterPresets)+1)
	if old.DirFilter != nil {
		// 正在用的那套排在最前面：它是用户此刻真的在跑的东西，默认也该先是它。
		out = append(out, FilterProfile{
			Name: freeName(DefaultFilterName, taken),
			Dirs: *old.DirFilter,
			// Files 留零值 = 不过滤文件。老文件里没有文件规则这回事，凭空给一套
			// 条件等于替用户决定"哪些文件不要"，而那些文件本来一直都在处理。
		})
	}
	for _, p := range old.FilterPresets {
		out = append(out, FilterProfile{Name: p.Name, Dirs: p.Filter})
	}
	s.FilterProfiles = out
	s.ActiveFilter = out[0].Name
}

// freeName picks a name nobody has taken yet: 「默认」/「默认 2」/「默认 3」……
//
// 老文件里完全可以有一套正好叫「默认」的方案。不避让的话，归一化会保留先出现的那个
// 并丢掉重名的后一个 —— 用户存了很久的那套会静默消失，而屏幕上多出来的那套看起来
// 就叫「默认」，谁也想不到是自己那套被顶掉了。
func freeName(base string, taken map[string]bool) string {
	if !taken[strings.ToLower(base)] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s %d", base, i)
		if !taken[strings.ToLower(cand)] {
			return cand
		}
	}
}
