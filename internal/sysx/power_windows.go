//go:build windows

package sysx

import (
	"runtime"
	"sync"
	"syscall"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000
)

// KeepAwake stops Windows from sleeping / turning the display off while a batch
// is running. SetThreadExecutionState only affects the calling thread, so we
// park a dedicated thread that holds the "system required" state until stop is
// called.
type KeepAwake struct {
	mu     sync.Mutex
	stop   chan struct{}
	done   chan struct{}
	active bool
}

// NewKeepAwake creates an idle keep-awake controller.
func NewKeepAwake() *KeepAwake { return &KeepAwake{} }

// Enable starts holding the sleep inhibitor. display=true also keeps the screen on.
func (k *KeepAwake) Enable(display bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.active {
		return
	}
	k.stop = make(chan struct{})
	k.done = make(chan struct{})
	stop, done := k.stop, k.done
	k.active = true

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		flags := uintptr(esContinuous | esSystemRequired)
		if display {
			flags |= esDisplayRequired
		}
		procSetThreadExecutionState.Call(flags)
		<-stop
		procSetThreadExecutionState.Call(uintptr(esContinuous))
	}()
}

// Disable releases the inhibitor if it is held.
func (k *KeepAwake) Disable() {
	k.mu.Lock()
	if !k.active {
		k.mu.Unlock()
		return
	}
	stop, done := k.stop, k.done
	k.active = false
	k.stop, k.done = nil, nil
	k.mu.Unlock()

	close(stop)
	<-done
}

// Active reports whether the inhibitor is currently held.
func (k *KeepAwake) Active() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.active
}
