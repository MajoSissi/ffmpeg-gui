package tray

import "testing"

// An empty string means "nothing to report" and has to land on the idle title.
// The tray is written on every queue event, so this is also what keeps a cleared
// queue from leaving the last count frozen on the icon.
func TestSetTooltipFallsBackToIdle(t *testing.T) {
	c := New(Callbacks{}, nil, "", "FFmpeg GUI — 媒体批量处理")
	if c.shown != "FFmpeg GUI — 媒体批量处理" {
		t.Fatalf("a fresh controller should start on the idle title, got %q", c.shown)
	}

	c.SetTooltip("FFmpeg GUI — 3/9 完成")
	if c.shown != "FFmpeg GUI — 3/9 完成" {
		t.Fatalf("tooltip = %q", c.shown)
	}

	c.SetTooltip("")
	if c.shown != "FFmpeg GUI — 媒体批量处理" {
		t.Errorf("an empty update must restore the idle title, got %q", c.shown)
	}
}

// Identical text is dropped rather than written again: every write is a
// synchronous Shell_NotifyIcon round trip to explorer, and the queue emits far
// more often than the text changes.
func TestSetTooltipLeavesTheTextAloneWhenUnchanged(t *testing.T) {
	c := New(Callbacks{}, nil, "", "idle")

	c.SetTooltip("one")
	c.SetTooltip("one")
	if c.shown != "one" {
		t.Fatalf("tooltip = %q, want %q", c.shown, "one")
	}
	// A different string still has to get through.
	c.SetTooltip("two")
	if c.shown != "two" {
		t.Errorf("tooltip = %q, want %q", c.shown, "two")
	}
}
