// Package ratelimit bounds how often one key may try something, in a table
// of keys that stays bounded whatever arrives.
//
// A key whose bucket has refilled is the same as a key never seen, so a full
// table forgets those first. When every key in a full table is still limited,
// a new key is refused rather than one of them forgotten: forgetting would
// give someone who spreads attempts over many keys a fresh bucket for the one
// key that matters.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter holds one token bucket per key.
type Limiter struct {
	every   time.Duration
	burst   int
	maxKeys int

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// New returns a limiter that allows burst attempts per key at once, one more
// every interval, for at most maxKeys keys.
func New(every time.Duration, burst, maxKeys int) *Limiter {
	return &Limiter{
		every:   every,
		burst:   max(burst, 1),
		maxKeys: max(maxKeys, 1),
		buckets: make(map[string]*rate.Limiter),
	}
}

// Allow takes one attempt for key at now and reports whether it was allowed.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys && !l.forgetRefilled(now) {
			return false
		}
		bucket = rate.NewLimiter(rate.Every(l.every), l.burst)
		l.buckets[key] = bucket
	}
	return bucket.AllowN(now, 1)
}

// Len is the number of keys the table holds.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// forgetRefilled drops every key whose bucket is full again, and reports
// whether it dropped any.
func (l *Limiter) forgetRefilled(now time.Time) bool {
	forgot := false
	for key, bucket := range l.buckets {
		if bucket.TokensAt(now) >= float64(l.burst) {
			delete(l.buckets, key)
			forgot = true
		}
	}
	return forgot
}
