//go:build !windows

package sysx

import "sync"

// KeepAwake is a no-op on non-Windows platforms.
type KeepAwake struct {
	mu     sync.Mutex
	active bool
}

// NewKeepAwake creates an idle keep-awake controller.
func NewKeepAwake() *KeepAwake { return &KeepAwake{} }

// Enable is a no-op outside Windows.
func (k *KeepAwake) Enable(display bool) {
	k.mu.Lock()
	k.active = true
	k.mu.Unlock()
}

// Disable is a no-op outside Windows.
func (k *KeepAwake) Disable() {
	k.mu.Lock()
	k.active = false
	k.mu.Unlock()
}

// Active reports whether the inhibitor is held.
func (k *KeepAwake) Active() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.active
}
