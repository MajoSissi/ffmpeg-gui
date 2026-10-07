// Package tray wraps the system tray icon with sensible menu actions.
package tray

import (
	"sync"

	"fyne.io/systray"
)

// Callbacks are invoked from the tray. OnShow fires both from the menu item and
// from a left click on the icon itself (see tray_windows.go).
//
// There is deliberately no pause/resume entry: pausing is a decision about what the
// queue is doing, and it belongs on the page that shows the queue, where you can see
// which tasks are running before you stop them. A one-word menu item 300px away from
// the table makes it too easy to freeze a batch you cannot see.
type Callbacks struct {
	OnShow func()
	OnQuit func()
	// OnReady fires once the icon and the menu are actually up. Anything that
	// writes to the icon before that is dropped by the library (it returns
	// ErrTrayNotReadyYet), so a tooltip describing the current queue has to be
	// pushed from here rather than from Start.
	OnReady func()
}

// Controller owns the tray lifecycle.
type Controller struct {
	cbs     Callbacks
	icon    []byte
	title   string
	tooltip string

	mu      sync.Mutex
	started bool
	stopped bool
	stopCh  chan struct{}
	// shown is the tooltip currently on the icon. Every queue event tries to
	// update the hover text, and each update is a synchronous Shell_NotifyIcon
	// round trip to explorer, so identical text is dropped here instead.
	shown string
}

// New builds a controller. icon must be ICO data on Windows.
func New(cbs Callbacks, icon []byte, title, tooltip string) *Controller {
	return &Controller{
		cbs:     cbs,
		icon:    icon,
		title:   title,
		tooltip: tooltip,
		shown:   tooltip,
		stopCh:  make(chan struct{}),
	}
}

// Start launches the tray in the background. Safe to call more than once.
func (c *Controller) Start() {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()
	// The left-click hook is installed from onReady instead: systray creates its
	// window inside Run, so there is nothing to subclass until then.
	go systray.Run(c.onReady, c.onExit)
}

// Stop tears the tray down.
func (c *Controller) Stop() {
	c.mu.Lock()
	if !c.started || c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	close(c.stopCh)
	c.mu.Unlock()
	// Give the tray window back before tearing the tray down: systray builds a
	// fresh window on the next Run, and 设置 → 托盘 off/on does exactly that.
	// Leaving the hook state behind made the restart skip the new window, which
	// is why clicking the icon stopped opening the window after a toggle.
	c.uninstallLeftClick()
	systray.Quit()
}

// SetTooltip updates the hover text (used to show queue progress). An empty
// string means "nothing to report" and restores the idle title.
func (c *Controller) SetTooltip(text string) {
	if text == "" {
		text = c.tooltip
	}
	c.mu.Lock()
	same := text == c.shown
	c.shown = text
	c.mu.Unlock()
	if same {
		return
	}
	systray.SetTooltip(text)
}

func (c *Controller) onReady() {
	// A missing icon is the classic "tray has no icon" bug: only call SetIcon
	// when we actually embedded a valid payload.
	if len(c.icon) > 0 {
		systray.SetIcon(c.icon)
	}
	systray.SetTitle(c.title)
	systray.SetTooltip(c.tooltip)

	// Now that the icon is up, take over left clicks so the icon itself opens the
	// window (tray_windows.go). Right click still gets the menu. No-op elsewhere.
	c.installLeftClick()

	mShow := systray.AddMenuItem("显示主界面", "打开 FFmpeg GUI 窗口")
	mQuit := systray.AddMenuItem("退出", "停止所有任务并退出程序")
	c.ready()

	go func() {
		for {
			select {
			case <-c.stopCh:
				return
			case <-mShow.ClickedCh:
				if c.cbs.OnShow != nil {
					c.cbs.OnShow()
				}
			case <-mQuit.ClickedCh:
				if c.cbs.OnQuit != nil {
					c.cbs.OnQuit()
				}
				return
			}
		}
	}()
}

func (c *Controller) onExit() {}

// ready is the tail of onReady: the icon, the hook and the menu are all up, so
// this is the first moment a tooltip write actually sticks.
func (c *Controller) ready() {
	if c.cbs.OnReady != nil {
		c.cbs.OnReady()
	}
}
