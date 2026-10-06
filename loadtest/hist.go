// Package loadtest is a game-agnostic WebSocket load-test harness: slots and
// ramp, histograms, per-second rates, disconnect reasons and the player loop
// (player.go).
package loadtest

import (
	"sync/atomic"
	"time"
)

// HistMax is the last finite bucket bound; slower observations land in the
// overflow bucket and read as HistMax+1 ms.
const HistMax = 1000

// Hist is a lock-free latency histogram with 1 ms buckets: bucket i holds
// observations in [i, i+1) ms, the last one everything >= HistMax ms.
// Writers Observe from any goroutine; the reporter reads counts.
type Hist struct {
	b [HistMax + 1]atomic.Uint64
}

func (h *Hist) Observe(d time.Duration) {
	i := int(d / time.Millisecond)
	if i < 0 {
		i = 0
	}
	if i > HistMax {
		i = HistMax
	}
	h.b[i].Add(1)
}

// Counts is a point-in-time copy of a Hist's buckets.
type Counts [HistMax + 1]uint64

func (h *Hist) Snapshot() Counts {
	var c Counts
	for i := range h.b {
		c[i] = h.b[i].Load()
	}
	return c
}

// Sub is c minus an older snapshot of the same Hist: the observations made
// in between.
func (c Counts) Sub(old Counts) Counts {
	var d Counts
	for i := range c {
		d[i] = c[i] - old[i]
	}
	return d
}

func (c Counts) Total() uint64 {
	var n uint64
	for _, v := range c {
		n += v
	}
	return n
}

// Quantile is the upper bound in ms of the bucket where the q-th
// observation falls (bucket i reads i+1); 0 with no observations.
func (c Counts) Quantile(q float64) int {
	total := c.Total()
	if total == 0 {
		return 0
	}
	rank := uint64(q*float64(total) + 0.999999)
	rank = max(rank, 1)
	var run uint64
	for i, v := range c {
		run += v
		if run >= rank {
			return i + 1
		}
	}
	return HistMax + 1
}

// Above counts observations of at least ms milliseconds.
func (c Counts) Above(ms int) uint64 {
	var n uint64
	for i := max(ms, 0); i < len(c); i++ {
		n += c[i]
	}
	return n
}
