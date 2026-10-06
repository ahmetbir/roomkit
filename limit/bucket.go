// Package limit holds the small rate and concurrency limiters the server
// uses against floods: a token bucket, a per-key bucket table with bounded
// memory, and a per-key connection gate. Time is passed in, never read.
package limit

import "time"

// Bucket is a token bucket: it refills at Rate tokens per second up to
// Burst. The zero Bucket is unusable; build one with NewBucket. Not safe for
// concurrent use — one owner goroutine.
type Bucket struct {
	rate, burst float64
	tokens      float64
	last        time.Time
}

// NewBucket returns a full bucket.
func NewBucket(rate float64, burst int) Bucket {
	return Bucket{rate: rate, burst: float64(burst), tokens: float64(burst)}
}

func (b *Bucket) refill(now time.Time) {
	if !b.last.IsZero() {
		if dt := now.Sub(b.last).Seconds(); dt > 0 {
			b.tokens = min(b.burst, b.tokens+dt*b.rate)
		}
	}
	b.last = now
}

// Allow takes one token if there is one.
func (b *Bucket) Allow(now time.Time) bool {
	b.refill(now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Refund gives back one token taken by Allow, up to Burst.
func (b *Bucket) Refund(now time.Time) {
	b.refill(now)
	b.tokens = min(b.burst, b.tokens+1)
}

// Empty reports, without taking a token, whether Allow would fail.
func (b *Bucket) Empty(now time.Time) bool {
	b.refill(now)
	return b.tokens < 1
}

// full reports whether the bucket holds as many tokens as a fresh one, so
// forgetting it changes nothing.
func (b *Bucket) full(now time.Time) bool {
	b.refill(now)
	return b.tokens >= b.burst
}
