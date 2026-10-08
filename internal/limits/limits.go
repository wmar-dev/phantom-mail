// Package limits provides per-key token-bucket rate limiting and
// trusted-proxy client address resolution.
package limits

import (
	"sync"
	"time"

	"phantom-mail/internal/clock"
)

// Limiter is a token-bucket rate limiter keyed by string (client address,
// mailbox name, ...). A nil *Limiter allows everything.
type Limiter struct {
	clk      clock.Clock
	perSec   float64
	capacity float64

	mu          sync.Mutex
	buckets     map[string]*bucket
	lastCleanup time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

const (
	cleanupEvery = time.Minute
	maxIdle      = 10 * time.Minute
)

// New returns a limiter allowing perMinute events per key with a burst of
// perMinute.
func New(perMinute int, clk clock.Clock) *Limiter {
	return &Limiter{
		clk:      clk,
		perSec:   float64(perMinute) / 60,
		capacity: float64(perMinute),
		buckets:  map[string]*bucket{},
	}
}

// Allow consumes one token for key and reports whether it was available.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastCleanup) >= cleanupEvery {
		l.cleanupLocked(now)
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.capacity, last: now}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.last).Seconds() * l.perSec
		if b.tokens > l.capacity {
			b.tokens = l.capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Peek reports whether key currently has a token available, without
// consuming one. It lets callers lock out a key that has used up its budget
// of failures, using Allow only when a failure actually happens.
func (l *Limiter) Peek(key string) bool {
	if l == nil {
		return true
	}
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		return true
	}
	tokens := b.tokens + now.Sub(b.last).Seconds()*l.perSec
	return tokens >= 1
}

// Cleanup drops buckets that have been idle long enough to be full again.
func (l *Limiter) Cleanup() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(l.clk.Now())
}

func (l *Limiter) cleanupLocked(now time.Time) {
	l.lastCleanup = now
	for k, b := range l.buckets {
		if now.Sub(b.last) >= maxIdle {
			delete(l.buckets, k)
		}
	}
}

// Len reports the number of tracked keys.
func (l *Limiter) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
