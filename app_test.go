package main

import (
	"strings"
	"testing"

	"ffmpeggui/internal/engine"
)

// The tray hover text is the thing the user sees when the window is out of the
// way, and it was wrong in two ways at once: it only ever got written on 开始 and
// 保存设置 (every other queue change went straight to a.emit, bypassing it), and
// it only got written while Total > 0, so clearing the queue left the last count
// frozen on the icon. Both are pinned here.
func TestTrayTooltipTracksQueueState(t *testing.T) {
	cases := []struct {
		name string
		st   engine.Stats
		want string
	}{
		{
			name: "空队列回落到空闲标题",
			st:   engine.Stats{},
			want: "",
		},
		{
			name: "刚拖进来还没开始",
			st:   engine.Stats{Total: 9, Pending: 9},
			want: "FFmpeg GUI — 0/9 完成 · 未开始",
		},
		{
			name: "处理中",
			st:   engine.Stats{Total: 9, Pending: 6, Running: 3, Started: true},
			want: "FFmpeg GUI — 0/9 完成 · 3 处理中",
		},
		{
			name: "暂停优先于处理中",
			st:   engine.Stats{Total: 9, Pending: 6, Running: 3, Started: true, Paused: true},
			want: "FFmpeg GUI — 0/9 完成 · 已暂停",
		},
		{
			// Removed rows count as finished, exactly like the progress bar does:
			// Stats.Finished is the one sum both of them read.
			name: "跳过与已取消都算完成",
			st:   engine.Stats{Total: 4, Done: 1, Skipped: 1, Canceled: 1, Failed: 1, Started: true},
			want: "FFmpeg GUI — 4/4 完成",
		},
		{
			name: "全部完成",
			st:   engine.Stats{Total: 2, Done: 1, Warning: 1, Started: true},
			want: "FFmpeg GUI — 2/2 完成",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trayTooltip(tc.st); got != tc.want {
				t.Errorf("trayTooltip = %q, want %q", got, tc.want)
			}
		})
	}
}

// The hover text is truncated by the shell at 128 UTF-16 units, and a tooltip that
// silently loses its tail is worse than a short one.
func TestTrayTooltipIsShortEnoughForTheShell(t *testing.T) {
	st := engine.Stats{Total: 100000, Done: 99999, Running: 1, Started: true}
	got := trayTooltip(st)
	if n := len([]rune(got)); n > 100 {
		t.Errorf("tooltip is %d chars, too long for the notification area: %q", n, got)
	}
	if !strings.Contains(got, "处理中") {
		t.Errorf("the queue state is missing from %q", got)
	}
}

// Finished is the one definition of "done" the progress bar and the tooltip share.
func TestStatsFinishedCountsEveryTerminalStatus(t *testing.T) {
	st := engine.Stats{Done: 1, Warning: 2, Failed: 3, Canceled: 4, Skipped: 5, Filtered: 6, Pending: 7, Running: 8}
	if got := st.Finished(); got != 21 {
		t.Errorf("Finished = %d, want 21 (pending and running must not count)", got)
	}
}
