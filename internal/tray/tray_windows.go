//go:build windows

package tray

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
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

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetClassNameW         = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW       = user32.NewProc("CallWindowProcW")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procGetCurrentProcessId   = kernel32.NewProc("GetCurrentProcessId")
)

const (
	msgSystray      = 0x0401 // WM_USER+1: the NOTIFYICONDATA callback message
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmNull          = 0x0000

	// GWLP_WNDPROC is -4. ^uintptr(3) is how -4 looks without a conversion error.
	gwlpWndProc = ^uintptr(3)
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
		// Only reachable in the instant between installing the hook and recording
		// the old procedure: swallow the one message rather than crash.
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(prev, hWnd, msg, wParam, lParam)

	// Forwarding a right-button release ran the library's menu path, which ends
	// in TrackPopupMenu and returns only once the menu is dismissed. TrackPopupMenu
	// leaves the shell mid-transaction unless a benign message is posted back to
	// the owner window afterwards -- without it, the *next* right click shows a
	// menu that vanishes the moment it appears, which reads as "right click
	// sometimes does nothing at all". Documented for notification icons in
	// MSDN's TrackPopupMenu remarks (originally KB135788), and fyne.io/systray
	// does the SetForegroundWindow half but never this one.
	if msg == msgSystray && lParam == wmRButtonUp {
		procPostMessageW.Call(hWnd, wmNull, 0, 0)
	}
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
			prev, _, _ := procSetWindowLongPtrW.Call(h, gwlpWndProc, trayWndProc)
			if prev != 0 {
				trayHookWnd.Store(h)
				trayPrevProc.Store(prev)
			}
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
	trayActivate.Store(nil)
	prev := trayPrevProc.Swap(0)
	if h := trayHookWnd.Swap(0); h != 0 && prev != 0 {
		// Fails harmlessly if the window is already destroyed.
		procSetWindowLongPtrW.Call(h, gwlpWndProc, prev)
	}
}
