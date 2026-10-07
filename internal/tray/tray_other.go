//go:build !windows

package tray

// installLeftClick is a no-op off Windows. Only fyne.io/systray's Windows
// backend turns a left button release on the icon into the context menu; the
// other platforms already do the sensible thing (or have no icon at all).
func (c *Controller) installLeftClick() {}

// uninstallLeftClick pairs with installLeftClick, so it is a no-op too.
func (c *Controller) uninstallLeftClick() {}
