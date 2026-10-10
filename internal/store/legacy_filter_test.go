package store

import (
	"encoding/json"
	"os"
	"testing"
)

// 老设置文件里那套"当前生效的目录条件"要变成一套有名字的方案。
//
// 用户手上那份 dirFilter 里是七条条件，方向、"含子目录"全都有。丢掉它，攒下来的条件
// 会在升级那一次一声不响地消失，而界面上只留下一套空规则 —— 用户看到的是"我的过滤
// 怎么没了"，而不是"升级了"。
func TestLegacyDirFilterBecomesAProfile(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	writeSettingsJSON(t, map[string]any{
		"dirFilter": map[string]any{
			"enabled": true, "matchAll": false, "exclude": true, "topOnly": true,
			"filters": []map[string]any{{"mode": "contains", "value": "1"}},
		},
		"filterPresets": []map[string]any{
			{"name": "批次", "filter": map[string]any{
				"enabled": true,
				"filters": []map[string]any{{"mode": "prefix", "value": "ffmpeg__"}},
			}},
		},
	})

	s := LoadSettings()
	if len(s.FilterProfiles) != 2 {
		t.Fatalf("翻出 %d 套方案（%+v），want 2", len(s.FilterProfiles), s.FilterProfiles)
	}
	if s.FilterProfiles[0].Name != DefaultFilterName {
		t.Errorf("正在用的那套排在最前面，得到 %q", s.FilterProfiles[0].Name)
	}
	d := s.FilterProfiles[0].Dirs
	if !d.Enabled || !d.Exclude || !d.TopOnly || len(d.Filters) != 1 || d.Filters[0].Value != "1" {
		t.Errorf("老条件没翻全：%+v", d)
	}
	// 老文件里没有文件规则这回事，所以那组必须是"不过滤" —— 凭空给一套条件等于替
	// 用户决定"哪些文件不要"，而那些文件本来一直都在处理。
	if s.FilterProfiles[0].Files.Active() {
		t.Error("老文件翻出来的方案不该带文件规则")
	}
	if s.FilterProfiles[1].Name != "批次" || s.FilterProfiles[1].Dirs.Filters[0].Value != "ffmpeg__" {
		t.Errorf("老方案的目录条件没翻对：%+v", s.FilterProfiles[1])
	}
	if s.ActiveFilter != DefaultFilterName {
		t.Errorf("默认生效的 = %q，want %q（老文件里正在用的那套）", s.ActiveFilter, DefaultFilterName)
	}

	// 翻译不写回：下一次保存只写新键，老键自然消失。
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	raw := readSettingsJSON(t)
	if _, ok := raw["dirFilter"]; ok {
		t.Error("保存之后不该还有 dirFilter 这个键")
	}
	if _, ok := raw["filterPresets"]; ok {
		t.Error("保存之后不该还有 filterPresets 这个键")
	}
	if _, ok := raw["filterProfiles"]; !ok {
		t.Error("保存之后应当只有 filterProfiles")
	}
}

// 老文件里正好有一套叫「默认」的方案时，翻译出来的那套要让开名字。
//
// 不让开的话，归一化会保留先出现的那个、丢掉重名的后一个 —— 用户存了很久的那套会静默
// 消失，而屏幕上多出来的那套看起来就叫「默认」，谁也想不到是自己那套被顶掉了。
func TestLegacyDirFilterYieldsItsName(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	writeSettingsJSON(t, map[string]any{
		"dirFilter": map[string]any{
			"enabled": true,
			"filters": []map[string]any{{"mode": "contains", "value": "在用"}},
		},
		"filterPresets": []map[string]any{
			{"name": DefaultFilterName, "filter": map[string]any{"enabled": true}},
		},
	})

	s := LoadSettings()
	if len(s.FilterProfiles) != 2 {
		t.Fatalf("翻出 %d 套方案（%+v），want 2", len(s.FilterProfiles), s.FilterProfiles)
	}
	// 用户自己那套「默认」（条件为空的那个）必须还在。
	kept := false
	for _, p := range s.FilterProfiles {
		if p.Name == DefaultFilterName && len(p.Dirs.Filters) == 0 {
			kept = true
		}
	}
	if !kept {
		t.Errorf("用户那套「默认」被顶掉了：%+v", s.FilterProfiles)
	}
	if s.FilterProfiles[0].Name == DefaultFilterName {
		t.Errorf("翻译出来的那套应当让开名字，得到 %+v", s.FilterProfiles[0])
	}
	if len(s.FilterProfiles[0].Dirs.Filters) != 1 || s.FilterProfiles[0].Dirs.Filters[0].Value != "在用" {
		t.Errorf("正在用的条件应当排在最前面，得到 %+v", s.FilterProfiles[0])
	}
}

// 只有新键的文件（全新安装）不该被翻出任何东西来。
func TestFreshSettingsGetNothingFromLegacy(t *testing.T) {
	defer SetDataDir(DataDir())
	SetDataDir(t.TempDir())

	writeSettingsJSON(t, map[string]any{"preventSleep": true})
	s := LoadSettings()
	if len(s.FilterProfiles) != 1 || s.FilterProfiles[0].Active() {
		t.Fatalf("全新安装应当只有一套空的方案，得到 %+v", s.FilterProfiles)
	}
	if s.ActiveFilter != DefaultFilterName {
		t.Errorf("ActiveFilter = %q, want %q", s.ActiveFilter, DefaultFilterName)
	}
}

// writeSettingsJSON 往设置文件里写一份指定内容的 json。
func writeSettingsJSON(t *testing.T, body map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SettingsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSettingsJSON(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
