// Package metrics is a dependency-free Prometheus text exposition: lock-free
// counters, gauges and fixed-bucket histograms, owned by one Registry that
// main builds and passes down (no globals).
package metrics

import (
	"fmt"
	"math"
	"slices"
	"sync/atomic"
)

type Counter struct{ v atomic.Uint64 }

func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Add(n uint64) { c.v.Add(n) }
func (c *Counter) Load() uint64 { return c.v.Load() }

type Gauge struct{ v atomic.Int64 }

func (g *Gauge) Add(d int64) { g.v.Add(d) }
func (g *Gauge) Set(v int64) { g.v.Store(v) }
func (g *Gauge) Load() int64 { return g.v.Load() }

// Histogram has fixed upper bounds; counts are per bucket (not cumulative)
// plus one +Inf bucket, and the sum is a float64 kept in its bits.
type Histogram struct {
	bounds  []float64
	counts  []atomic.Uint64 // len(bounds)+1, last is +Inf
	sum     atomic.Uint64
	invalid atomic.Uint64 // NaN/±Inf observations, ignored
}

// NewHistogram takes finite upper bounds; they are sorted and de-duplicated.
func NewHistogram(bounds []float64) *Histogram {
	b := make([]float64, 0, len(bounds))
	for _, x := range bounds {
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			b = append(b, x)
		}
	}
	slices.Sort(b)
	b = slices.Compact(b)
	return &Histogram{bounds: b, counts: make([]atomic.Uint64, len(b)+1)}
}

// Observe records v; NaN and ±Inf are ignored and counted (see Invalid) so
// they can never poison _sum.
func (h *Histogram) Observe(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		h.invalid.Add(1)
		return
	}
	i := 0
	for i < len(h.bounds) && v > h.bounds[i] {
		i++
	}
	h.counts[i].Add(1)
	for {
		old := h.sum.Load()
		if h.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

// Count is the number of finite observations.
func (h *Histogram) Count() uint64 {
	var n uint64
	for i := range h.counts {
		n += h.counts[i].Load()
	}
	return n
}

// Invalid is the number of NaN/±Inf observations that were ignored.
func (h *Histogram) Invalid() uint64 { return h.invalid.Load() }

// Quantile is the upper bound of the first bucket whose cumulative count
// reaches q of the total; 0 with no observations. Past the last bound it is
// +Inf, so Summary prints tick_p99_ms=+Inf once p99 exceeds 50 ms.
func (h *Histogram) Quantile(q float64) float64 {
	snap := h.snapshot()
	total := snap[len(snap)-1]
	if total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	rank = max(rank, 1)
	for i, c := range snap[:len(h.bounds)] {
		if c >= rank {
			return h.bounds[i]
		}
	}
	return math.Inf(1)
}

// snapshot returns cumulative counts, one per bound plus +Inf (the total).
func (h *Histogram) snapshot() []uint64 {
	out := make([]uint64, len(h.counts))
	var run uint64
	for i := range h.counts {
		run += h.counts[i].Load()
		out[i] = run
	}
	return out
}

func (h *Histogram) sumValue() float64 { return math.Float64frombits(h.sum.Load()) }

// CounterVec is a counter per value of one label. The value set is fixed at
// construction; anything else is counted under "other", so callers cannot grow it.
type CounterVec struct {
	label  string
	values []string // exposition order; "other" last
	byVal  map[string]*Counter
}

func NewCounterVec(label string, values ...string) *CounterVec {
	v := &CounterVec{label: label, byVal: make(map[string]*Counter, len(values)+1)}
	for _, s := range values {
		v.add(s)
	}
	v.add("other")
	return v
}

func (v *CounterVec) add(s string) {
	if _, dup := v.byVal[s]; !dup {
		v.values = append(v.values, s)
		v.byVal[s] = &Counter{}
	}
}

func (v *CounterVec) Inc(value string) {
	c, ok := v.byVal[value]
	if !ok {
		c = v.byVal["other"]
	}
	c.Inc()
}

// Registry holds one server's metrics.
type Registry struct {
	ns                                       string // name prefix, e.g. "dogfight"
	Rooms, Humans, Bots, Conns               Gauge
	TickSeconds                              *Histogram
	TickOverruns, MsgsIn, MsgsOut, SnapDrops Counter
	Rejects                                  *CounterVec   // reason
	API                                      *CounterVec   // path
	StatsDropped                             func() uint64 // optional, read at scrape
}

func New(namespace string, statsDropped func() uint64) *Registry {
	return &Registry{
		ns: namespace,
		// tick buckets in seconds: 1, 2, 4, 8, 12, 16.7, 25, 50 ms (spec §11)
		TickSeconds: NewHistogram([]float64{0.001, 0.002, 0.004, 0.008, 0.012, 0.0167, 0.025, 0.05}),
		Rejects: NewCounterVec("reason", "conns-per-ip", "conns-per-net", "conns-total", "create-rate", "join-rate",
			"join-fail-rate", "limiter-full", "max-rooms", "flood", "api-rate"),
		API:          NewCounterVec("path", "rooms", "leaderboard", "me"),
		StatsDropped: statsDropped,
	}
}

// Summary is the one-line periodic log: drops are snapshot drops.
func (r *Registry) Summary() string {
	return fmt.Sprintf("rooms=%d humans=%d bots=%d conns=%d tick_p99_ms=%.1f overruns=%d drops=%d",
		r.Rooms.Load(), r.Humans.Load(), r.Bots.Load(), r.Conns.Load(),
		r.TickSeconds.Quantile(0.99)*1000, r.TickOverruns.Load(), r.SnapDrops.Load())
}
