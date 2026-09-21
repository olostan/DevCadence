// Package clock provides the time abstraction used by the control plane.
//
// ENGINEERING_STANDARDS.md §17 requires deterministic behaviour for identical
// durable inputs. Wall-clock reads are therefore never taken directly by
// domain or storage code; they are taken from an injected Clock so that tests
// can supply a deterministic sequence of timestamps.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time. Implementations must return UTC times.
type Clock interface {
	Now() time.Time
}

// SystemClock reads the operating-system wall clock.
type SystemClock struct{}

// System is the clock used outside tests.
func System() Clock { return SystemClock{} }

// Now returns the current UTC time truncated to microsecond resolution.
//
// Truncation keeps round-trips through RFC3339 text and SQLite stable: a
// timestamp that is written and read back must compare equal, otherwise
// projections rebuilt from durable records would not match the originals.
func (SystemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Fake is a deterministic Clock for tests. It starts at a fixed instant and
// advances by Step on every read, so a test that performs N timed operations
// produces N distinct, predictable timestamps without sleeping.
//
// Fake is safe for concurrent use.
type Fake struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

// NewFake returns a Fake starting at start and advancing by step per read.
// A zero step keeps the clock frozen.
func NewFake(start time.Time, step time.Duration) *Fake {
	return &Fake{now: start.UTC().Truncate(time.Microsecond), step: step}
}

// Now returns the current fake time and then advances it by the step.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	current := f.now
	f.now = f.now.Add(f.step)
	return current
}

// Advance moves the fake clock forward without consuming a reading.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Peek reports the instant the next Now would return.
func (f *Fake) Peek() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}
