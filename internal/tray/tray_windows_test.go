//go:build windows

package tray

import (
	"testing"
	"time"
)

// The tooltip is written from queue events, which arrive on worker goroutines.
// They all land on the same slot and only the newest survives, so a burst costs
// the shell one write instead of one per event. The tray thread pops it.
func TestPendingTipKeepsOnlyTheNewest(t *testing.T) {
	tipWant.Store(nil)
	t.Cleanup(func() { tipWant.Store(nil) })

	if _, ok := takePendingTip(); ok {
		t.Fatal("nothing was requested, but a write came out of it")
	}

	a, b := "1/9 完成", "2/9 完成"
	tipWant.Store(&a)
	tipWant.Store(&b) // the queue moved on before the tray thread ran

	if got, ok := takePendingTip(); !ok || got != b {
		t.Fatalf("takePendingTip = %q, %v; want %q, true", got, ok, b)
	}
	// Second pop finds nothing: the ping that arrived in between must not
	// replay a value that is already on the icon.
	if got, ok := takePendingTip(); ok {
		t.Fatalf("the slot should be empty after a pop, got %q", got)
	}
}

// The throttle is what keeps a fast queue from parking the tray thread inside
// the shell, where it cannot answer a right click.
func TestTipBackoffHoldsUpdatesBackToTheMinimumGap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	if d := tipBackoff(0, now); d != 0 {
		t.Errorf("first write of the run waited %v, want none", d)
	}
	if d := tipBackoff(now.UnixNano(), now); d != tipMinGap {
		t.Errorf("write straight after another waited %v, want %v", d, tipMinGap)
	}
	if d := tipBackoff(now.Add(-tipMinGap).UnixNano(), now); d != 0 {
		t.Errorf("write exactly one gap later waited %v, want none", d)
	}
	if d := tipBackoff(now.Add(-time.Second).UnixNano(), now); d != 0 {
		t.Errorf("write after a quiet second waited %v, want none", d)
	}
	// The wait has to shrink as the gap is used up, or the flush timer would be
	// scheduled for a moment that has already passed.
	half := now.Add(-tipMinGap / 2)
	if d := tipBackoff(half.UnixNano(), now); d <= 0 || d >= tipMinGap {
		t.Errorf("halfway through the gap the wait is %v, want between 0 and %v", d, tipMinGap)
	}
}

// Nothing may reach the shell while the menu's modal loop is on the stack: that
// is the cross-process wait this whole detour exists to avoid.
func TestPendingTipSurvivesAnOpenMenu(t *testing.T) {
	tipWant.Store(nil)
	menuOpen.Store(true)
	t.Cleanup(func() {
		tipWant.Store(nil)
		menuOpen.Store(false)
	})

	want := "3/9 完成"
	tipWant.Store(&want)

	applyPendingTip() // must hold the value back rather than write it

	if got, ok := takePendingTip(); !ok || got != want {
		t.Fatalf("the tooltip was dropped while the menu was open: %q, %v", got, ok)
	}
}
