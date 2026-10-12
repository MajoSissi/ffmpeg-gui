//go:build !windows

package tray

import "fyne.io/systray"

// installLeftClick is a no-op off Windows. Only fyne.io/systray's Windows
// backend turns a left button release on the icon into the context menu; the
// other platforms already do the sensible thing (or have no icon at all).
func (c *Controller) installLeftClick() {}

// uninstallLeftClick pairs with installLeftClick, so it is a no-op too.
func (c *Controller) uninstallLeftClick() {}

// applyTooltip writes the hover text straight through. The detour through the
// tray window exists for Windows only: it is the shell's synchronous
// notification API, called from the wrong thread, that misbehaves there.
func (c *Controller) applyTooltip(text string) {
	if c.isStopped() {
		return
	}
	systray.SetTooltip(text)
}
