// Package clock abstracts time so retention, rate limiting, and IDs are
// deterministic in tests.
package clock

import (
	"sync"
	"time"
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Real is the wall clock.
type Real struct{}

// Now returns the current time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock for tests.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a Fake starting at t.
func NewFake(t time.Time) *Fake { return &Fake{t: t} }

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Advance moves the fake time forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

// Set replaces the fake time.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.t = t
	f.mu.Unlock()
}
