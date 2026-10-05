package engine

import (
	"context"
	"sync"
)

// throttle caps how many jobs of the same template run at the same time.
//
// The worker pool is sized by the global template's concurrency, so a per-template
// value can only lower the effective parallelism. A template asking for more than
// the ceiling is clamped by the caller.
//
// It deliberately does not share the runner's condition variable: that one guards
// the queue, and a slot release waking a queue waiter that then re-blocks would
// stall the whole batch on a single missed wakeup. Waiters here are instead parked
// on a "wake" channel that every release closes, so they can also be released by
// the job's own context -- a job still waiting for a slot has not started yet and
// therefore has no ffmpeg process to cancel.
type throttle struct {
	mu      sync.Mutex
	used    map[string]int
	stopped bool
	// wake is closed and replaced whenever a slot frees or the throttle is
	// stopped. Waiters snapshot it under mu and select on it.
	wake chan struct{}
}

func newThrottle() *throttle {
	return &throttle{used: map[string]int{}, wake: make(chan struct{})}
}

// signalLocked wakes every waiter. Callers must hold mu.
func (t *throttle) signalLocked() {
	close(t.wake)
	t.wake = make(chan struct{})
}

// acquire blocks until fewer than limit jobs of that template are running, then
// takes a slot of that weight. It reports false when ctx was cancelled or the
// throttle was stopped -- either way the caller must abandon the job.
func (t *throttle) acquire(ctx context.Context, key string, limit int) bool {
	if limit < 1 {
		limit = 1
	}
	for {
		t.mu.Lock()
		if t.stopped {
			t.mu.Unlock()
			return false
		}
		if t.used[key] < limit {
			t.used[key]++
			t.mu.Unlock()
			return true
		}
		wake := t.wake
		t.mu.Unlock()

		select {
		case <-wake:
			// A slot may have freed; loop re-checks under the lock. Spurious
			// wakeups (several waiters racing for one slot) are harmless.
		case <-ctx.Done():
			return false
		}
	}
}

// release gives the slots back and wakes anyone waiting.
func (t *throttle) release(key string, n int) {
	if n < 1 {
		n = 1
	}
	t.mu.Lock()
	t.used[key] -= n
	if t.used[key] < 0 {
		t.used[key] = 0
	}
	t.signalLocked()
	t.mu.Unlock()
}

// stop wakes every waiter and makes further acquires fail. Called when the queue
// is stopped so waiting jobs do not hang until their turn comes.
func (t *throttle) stop() {
	t.mu.Lock()
	t.stopped = true
	t.signalLocked()
	t.mu.Unlock()
}

// restart re-arms the throttle after the queue is started again.
func (t *throttle) restart() {
	t.mu.Lock()
	t.stopped = false
	t.used = map[string]int{}
	t.signalLocked()
	t.mu.Unlock()
}
