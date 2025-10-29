package main

import (
	"sync"
	"time"
)

// Debouncer batches rapid events into a single action after a quiet period.
// Thread-safe for concurrent triggers.
type Debouncer struct {
	mu       sync.Mutex
	timer    *time.Timer
	duration time.Duration
	action   func()
}

// NewDebouncer creates a new debouncer with the given duration and action.
// The action will be called once after the duration has passed since the last trigger.
func NewDebouncer(duration time.Duration, action func()) *Debouncer {
	return &Debouncer{
		duration: duration,
		action:   action,
	}
}

// Trigger schedules the action to run after the debounce duration.
// If called multiple times, the timer is reset each time, ensuring
// the action only fires once after the last trigger.
func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.duration, func() {
		d.action()
		d.mu.Lock()
		d.timer = nil
		d.mu.Unlock()
	})
}

// Cancel stops any pending debounced action.
// Safe to call even if no action is pending.
func (d *Debouncer) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}
