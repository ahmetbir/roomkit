package limit

import (
	"sync"
	"time"
)

const (
	sweepEvery = time.Minute
	// DefaultMaxKeys bounds a Keyed table; past it, unknown keys are refused
	// until a sweep frees room (only reachable under a wide address flood).
	DefaultMaxKeys = 1 << 16
)

// Keyed is a table of token buckets, one per key (client IP). A bucket that
// has refilled completely carries no state and is dropped by the periodic
// sweep, so memory follows recent activity, never history. Safe for
// concurrent use.
type Keyed struct {
	rate    float64
	burst   int
	maxKeys int

	mu        sync.Mutex
	m         map[string]*Bucket
	lastSweep time.Time
}

// NewKeyed allows burst events per key, refilling at perMinute/60 per second.
func NewKeyed(perMinute float64, burst int) *Keyed {
	return &Keyed{rate: perMinute / 60, burst: burst, maxKeys: DefaultMaxKeys, m: map[string]*Bucket{}}
}

// Allow takes one token from key's bucket.
func (k *Keyed) Allow(key string, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	b := k.bucket(key, now)
	return b != nil && b.Allow(now)
}

// Refund gives back a token that Allow took for key, for an action that
// did not happen. Unknown keys are left alone.
func (k *Keyed) Refund(key string, now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if b, ok := k.m[key]; ok {
		b.Refund(now)
	}
}

// Known reports whether key holds state in the table.
func (k *Keyed) Known(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.m[key]
	return ok
}

// Blocked reports, without taking a token, whether key is out of tokens.
func (k *Keyed) Blocked(key string, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	b, ok := k.m[key]
	if !ok {
		k.sweep(now)
		return len(k.m) >= k.maxKeys
	}
	return b.Empty(now)
}

// Full reports whether the table is at capacity and refuses new keys.
func (k *Keyed) Full() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.m) >= k.maxKeys
}

// Len is the number of keys holding state.
func (k *Keyed) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.m)
}

// bucket finds or adds key's bucket; nil when the table is full. Needs k.mu.
func (k *Keyed) bucket(key string, now time.Time) *Bucket {
	k.sweep(now)
	if b, ok := k.m[key]; ok {
		return b
	}
	if len(k.m) >= k.maxKeys {
		return nil
	}
	b := NewBucket(k.rate, k.burst)
	k.m[key] = &b
	return &b
}

// sweep drops full buckets at most once per sweepEvery. Needs k.mu.
func (k *Keyed) sweep(now time.Time) {
	if now.Sub(k.lastSweep) < sweepEvery {
		return
	}
	k.lastSweep = now
	for key, b := range k.m {
		if b.full(now) {
			delete(k.m, key)
		}
	}
}
