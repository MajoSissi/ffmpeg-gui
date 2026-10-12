//go:build windows

package tray

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/systray"
)

// Left-click activation. fyne.io/systray's Windows wndProc turns *every* button
// release on the icon — left included — into showMenu (systray_windows.go:328),
// so a click could only ever open the menu and the main window needed a round
// trip through it. The library exposes no hook for this, so the tray window is
// subclassed the classic Win32 way: swap the window procedure, watch for the
// icon-callback message carrying a left button release (or double click), and
// forward everything else untouched.
//
// The icon-callback message is wmSystrayMessage = WM_USER+1, a fixed value in
// the library, and the window class is the equally fixed "SystrayClass" — both
// are what make this possible without patching the module.
//
// The same window doubles as the place every icon write goes through. systray
// writes straight to the shell (Shell_NotifyIcon) from whichever goroutine
// calls it, and ours is called on every queue event — a worker thread. A
// synchronous cross-process write to explorer, issued while the shell is trying
// to call *into* the icon's window proc, is how a right click ends up doing
// nothing at all; see applyTooltip.

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetClassNameW         = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW       = user32.NewProc("CallWindowProcW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procGetCurrentProcessId   = kernel32.NewProc("GetCurrentProcessId")
)

const (
	msgSystray      = 0x0401 // WM_USER+1: the NOTIFYICONDATA callback message
	msgTraySetTip   = 0x8001 // WM_APP+1: our own "write the pending tooltip" ping
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmNull          = 0x0000

	// GWLP_WNDPROC is -4. ^uintptr(3) is how -4 looks without a conversion error.
	gwlpWndProc = ^uintptr(3)

	// tipMinGap is the shortest interval between two Shell_NotifyIcon writes.
	// The hover text carries a count, and a busy queue changes it several times
	// a second; without a floor the tray thread spends the whole batch inside
	// the shell and is not free to answer a click. Updates arriving inside the
	// gap are not dropped, only held back — the newest one goes out at the end
	// of it.
	tipMinGap = 200 * time.Millisecond
)

var (
	// trayActivate is the callback the left-click hook runs. An atomic pointer
	// rather than a plain variable because the message-loop thread reads it
	// while onReady writes it, and because uninstallLeftClick has to clear it.
	trayActivate atomic.Pointer[func()]
	trayPrevProc atomic.Uintptr // the procedure we replaced, for forwarding
	trayHookWnd  atomic.Uintptr // the window the hook is installed on
	trayWndProc  = syscall.NewCallback(trayWindowProc)
	trayEnumProc = syscall.NewCallback(trayEnumWindow)

	// tipWant is the newest hover text waiting for the tray thread. Any
	// goroutine may write it; only the tray thread consumes it, so a burst of
	// queue events collapses into a single write of the value that is current.
	tipWant  atomic.Pointer[string]
	tipLast  atomic.Int64 // UnixNano of the last successful write
	tipTimer atomic.Bool  // a trailing flush is already scheduled
	// menuOpen is set from the tray thread for the whole of showMenu, i.e. for
	// as long as TrackPopupMenu's modal loop is on the stack.
	menuOpen atomic.Bool

	trayClass    = []uint16{} // "SystrayClass", filled in init to avoid an error path
	scanPID      uintptr      // process id we are looking for during a scan
	scanFoundWnd uintptr      // first matching window, written by the enum callback
)

func init() {
	if s, err := syscall.UTF16FromString("SystrayClass"); err == nil {
		trayClass = s
	}
}

// trayWindowProc is the subclass procedure. Windows calls it on the tray's own
// message-loop thread, so everything here must be lock-free and fast.
func trayWindowProc(hWnd, msg, wParam, lParam uintptr) uintptr {
	if msg == msgTraySetTip {
		// Our own ping, sent by whichever goroutine last changed the tooltip.
		// Handling it here is the point of it existing: this thread owns the
		// icon, and it is the one MSDN expects Shell_NotifyIcon to be called
		// from. Never forwarded — the library has no idea what WM_APP+1 is.
		applyPendingTip()
		return 0
	}
	if msg == msgSystray && hWnd == trayHookWnd.Load() &&
		(lParam == wmLButtonUp || lParam == wmLButtonDblClk) {
		// A double click also produces a plain release first; showing the window
		// twice is harmless, so both are handled the same way.
		//
		// Showing the window goes through Wails, which pins an OS thread and
		// round-trips to the main window's message loop. Doing that inline would
		// stall the tray's own message loop for as long as it takes, so it is
		// handed to a goroutine — this procedure stays non-blocking.
		if p := trayActivate.Load(); p != nil {
			if fn := *p; fn != nil {
				go fn()
			}
		}
		return 0
	}

	prev := trayPrevProc.Load()
	if prev == 0 {
		// Only reachable in the instant between installing the hook and
		// recording the old procedure. Falling through to DefWindowProc is
		// deliberate: returning 0 here swallows the message, and a swallowed
		// icon callback is a click that visibly does nothing.
		r, _, _ := procDefWindowProcW.Call(hWnd, msg, wParam, lParam)
		return r
	}

	// Forwarding a right-button release runs the library's menu path, which ends
	// in TrackPopupMenu: a modal loop that does not return until the menu is
	// dismissed. Two things follow from that, and both are handled below.
	if msg == msgSystray && hWnd == trayHookWnd.Load() && lParam == wmRButtonUp {
		// 1. While the menu is up, the shell must be able to call into this
		//    window proc. Issuing our own Shell_NotifyIcon meanwhile — which
		//    blocks on explorer, which is waiting on us — is the deadlock that
		//    makes the menu look stuck. Tooltip writes are held back for the
		//    duration and flushed when the menu is done.
		menuOpen.Store(true)
		r, _, _ := procCallWindowProcW.Call(prev, hWnd, msg, wParam, lParam)
		menuOpen.Store(false)

		// 2. TrackPopupMenu leaves the shell mid-transaction unless a benign
		//    message is posted back to the owner window afterwards — without it,
		//    the *next* right click shows a menu that vanishes the moment it
		//    appears, which reads as "right click sometimes does nothing at
		//    all". Documented for notification icons in MSDN's TrackPopupMenu
		//    remarks (originally KB135788), and fyne.io/systray does the
		//    SetForegroundWindow half but never this one. It has to be posted
		//    *after* the call returns, which is exactly here.
		procPostMessageW.Call(hWnd, wmNull, 0, 0)
		applyPendingTip()
		return r
	}

	r, _, _ := procCallWindowProcW.Call(prev, hWnd, msg, wParam, lParam)
	return r
}

// trayEnumWindow collects the tray window. It has to compare process ids and not
// just the class name: FindWindowW happily returns another process's tray window
// — a previous instance that has not exited yet is enough — and subclassing that
// would install the hook somewhere the clicks never arrive.
func trayEnumWindow(hWnd, _ uintptr) uintptr {
	if scanFoundWnd != 0 || len(trayClass) == 0 {
		return 0 // already got one; stop
	}
	var cls [64]uint16
	if n, _, _ := procGetClassNameW.Call(hWnd, uintptr(unsafe.Pointer(&cls[0])), 64); n == 0 {
		return 1
	}
	if !utf16Eq(cls[:], trayClass) {
		return 1
	}
	var pid uintptr
	procGetWindowThreadProcId.Call(hWnd, uintptr(unsafe.Pointer(&pid)))
	if pid != scanPID {
		return 1
	}
	scanFoundWnd = hWnd
	return 0
}

func utf16Eq(a, b []uint16) bool {
	ia := indexOfNul(a)
	ib := indexOfNul(b)
	if ia != ib {
		return false
	}
	for i := 0; i < ia; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOfNul(a []uint16) int {
	for i, v := range a {
		if v == 0 {
			return i
		}
	}
	return len(a)
}

/**
 * applyTooltip hands the new hover text to the tray thread instead of writing it
 * here.
 *
 * SetTooltip is reached from every queue event, so in practice from worker
 * goroutines, the history saver and the Wails main thread — not from the thread
 * that owns the icon. systray's SetTooltip is a synchronous Shell_NotifyIcon
 * round trip to explorer, and issuing one of those from a foreign thread while
 * the shell is trying to call into the icon's window proc is a deadlock: our
 * thread waits on the shell, the shell waits on the window, and the right click
 * that is supposed to arrive in between never does. It shows up as the menu
 * occasionally not opening at all, or explorer hanging on to the app.
 *
 * So the text is parked in tipWant and a message is posted. The tray thread
 * picks it up in applyPendingTip, which is also where the coalescing and the
 * throttling happen — neither is possible from out here, because neither can
 * know what has already been written.
 */
func (c *Controller) applyTooltip(text string) {
	if c.isStopped() {
		return
	}
	h := trayHookWnd.Load()
	if h == 0 {
		// No tray window to post to: either onReady has not run yet or the
		// subclass never went in. Fall back to the direct call so the text is
		// not silently lost; once the hook is up this branch is unreachable.
		systray.SetTooltip(text)
		return
	}
	tipWant.Store(&text)
	procPostMessageW.Call(h, msgTraySetTip, 0, 0)
}

// applyPendingTip writes the newest requested tooltip to the icon. Runs on the
// tray's own thread, from the message procedure, and nowhere else.
func applyPendingTip() {
	if menuOpen.Load() {
		// The menu's modal loop is on the stack. Skip the shell call and leave
		// the value parked: the right-click branch flushes it on the way out,
		// and a later event would repost it anyway.
		return
	}
	now := time.Now()
	if wait := tipBackoff(tipLast.Load(), now); wait > 0 {
		// Too soon after the previous write. Nothing is dropped — the value
		// stays in tipWant and the timer below comes back for it.
		scheduleTipFlush(wait)
		return
	}
	text, ok := takePendingTip()
	if !ok {
		return
	}
	tipLast.Store(now.UnixNano())
	systray.SetTooltip(text)
}

// tipBackoff is how long the next write still has to wait; 0 means it can go
// out now. lastNanos is a UnixNano stamp, and 0 — nothing written yet this run
// — never waits. time.Time.IsZero is no use here: time.Unix(0, 0) is 1970.
func tipBackoff(lastNanos int64, now time.Time) time.Duration {
	if lastNanos == 0 {
		return 0
	}
	if d := tipMinGap - now.Sub(time.Unix(0, lastNanos)); d > 0 {
		return d
	}
	return 0
}

// takePendingTip pops the newest tooltip waiting to be written. Every queued
// ping races for the same slot, so all but the first of them find it empty and
// return — that is the coalescing, and it is why a burst of queue events costs
// one write rather than one write each.
func takePendingTip() (string, bool) {
	if p := tipWant.Swap(nil); p != nil {
		return *p, true
	}
	return "", false
}

// scheduleTipFlush posts a ping once the throttle window has passed, unless one
// is already pending.
func scheduleTipFlush(wait time.Duration) {
	if !tipTimer.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(wait, func() {
		tipTimer.Store(false)
		if h := trayHookWnd.Load(); h != 0 {
			procPostMessageW.Call(h, msgTraySetTip, 0, 0)
		}
	})
}

/**
 * installLeftClick subclasses the tray window so that clicking the icon -- once or
 * twice -- opens the main window instead of popping the menu.
 *
 * It is called from onReady rather than from Start: systray creates its window
 * inside systray.Run, so installing from Start would mean polling for a window
 * that does not exist yet. By the time onReady fires the window and the icon are
 * both up. onReady runs on its own goroutine though, which can overlap the
 * message loop, hence the atomic below rather than a plain variable.
 */
func (c *Controller) installLeftClick() {
	if trayPrevProc.Load() != 0 || len(trayClass) == 0 {
		return // already subclassed, or the class name could not be encoded
	}
	fn := c.cbs.OnShow
	if fn == nil {
		return
	}
	// Set the callback before installing: a message could arrive the instant the
	// swap takes effect.
	trayActivate.Store(&fn)

	self, _, _ := procGetCurrentProcessId.Call()
	for i := 0; i < 20; i++ {
		scanPID, scanFoundWnd = self, 0
		procEnumWindows.Call(trayEnumProc, 0)
		if h := scanFoundWnd; h != 0 {
			// Read the current procedure *before* swapping it in. Taking it from
			// SetWindowLongPtrW's return value instead looks equivalent but is
			// not: on failure that returns 0, and the code then left a window
			// running our procedure with nothing to forward to — the permanent
			// state where every tray message, right clicks included, goes
			// nowhere and the icon looks dead until the app restarts.
			prev, _, _ := procGetWindowLongPtrW.Call(h, gwlpWndProc)
			if prev == 0 {
				return // could not read it; leave the window alone
			}
			// Publish the window first: the left-click branch only needs hWnd,
			// so it works during the instant below. Everything else falls into
			// the proc's prev==0 path, which defers to DefWindowProc rather than
			// swallowing the message.
			trayHookWnd.Store(h)
			procSetWindowLongPtrW.Call(h, gwlpWndProc, trayWndProc)
			trayPrevProc.Store(prev)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// No window turned up. Drop the callback rather than leave one pointing at a
	// controller that may be gone by the time a click arrives.
	trayActivate.Store(nil)
}

// uninstallLeftClick hands the tray window back to the library's procedure and
// clears the hook state so a later Start can install it again.
//
// It runs from Stop, which is what 设置 → 托盘 off/on does. Without it, the next
// Start found trayPrevProc already set, skipped the (brand new) tray window, and
// clicking the icon quietly stopped opening the window -- the hook was still
// pointing at a window that no longer existed.
func (c *Controller) uninstallLeftClick() {
	h := trayHookWnd.Load()
	prev := trayPrevProc.Load()
	if h != 0 && prev != 0 {
		// Fails harmlessly if the window is already destroyed.
		procSetWindowLongPtrW.Call(h, gwlpWndProc, prev)
	}
	// Cleared only after the window is back on the library's procedure. The
	// other order leaves a window still pointed at our subclass with nothing to
	// forward to, and every message arriving in between is swallowed — a right
	// click that does nothing, in the one moment the user is least able to tell
	// it apart from the tray being off.
	trayHookWnd.Store(0)
	trayPrevProc.Store(0)
	trayActivate.Store(nil)
	// Anything the tray still owed the icon dies with it.
	tipWant.Store(nil)
	tipTimer.Store(false)
	menuOpen.Store(false)
}
