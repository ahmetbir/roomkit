package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Stats is the run's shared tally. Players write it lock-free (atomics and
// hists); only Disconnect reasons, rare, take the lock.
type Stats struct {
	Conns                    atomic.Int64 // players past the handshake, still connected
	MsgsIn, MsgsOut, BytesIn atomic.Uint64
	BytesOut, Snaps, Gaps    atomic.Uint64
	SnapIv, RTT, Handshake   Hist
	mu                       sync.Mutex
	reasons                  map[string]int
}

func NewStats() *Stats { return &Stats{reasons: map[string]int{}} }

func (s *Stats) Disconnect(reason string) {
	s.mu.Lock()
	s.reasons[reason]++
	s.mu.Unlock()
}

func (s *Stats) ReasonCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.reasons)
}

// Sample is a point-in-time copy of the cumulative tally.
type Sample struct {
	At                       time.Duration
	conns                    int64
	msgsIn, msgsOut, bytesIn uint64
	bytesOut, snaps, gaps    uint64
	snapIv, rtt              Counts
	disc                     int
}

func (s *Stats) Sample(at time.Duration) Sample {
	s.mu.Lock()
	disc := 0
	for _, n := range s.reasons {
		disc += n
	}
	s.mu.Unlock()
	return Sample{
		At: at, conns: s.Conns.Load(),
		msgsIn: s.MsgsIn.Load(), msgsOut: s.MsgsOut.Load(), bytesIn: s.BytesIn.Load(),
		bytesOut: s.BytesOut.Load(), snaps: s.Snaps.Load(), gaps: s.Gaps.Load(),
		snapIv: s.SnapIv.Snapshot(), rtt: s.RTT.Snapshot(), disc: disc,
	}
}

// line is the per-interval report between two samples. "in" is what the
// players received (the server's outbound), "out" what they sent;
// rx/client is the server's outbound bandwidth per connected player.
func Line(prev, cur Sample) string {
	dt := (cur.At - prev.At).Seconds()
	if dt <= 0 {
		dt = 1
	}
	rate := func(a, b uint64) float64 { return float64(a-b) / dt }
	perClient := 0.0
	if cur.conns > 0 {
		perClient = rate(cur.bytesIn, prev.bytesIn) / float64(cur.conns) / 1024
	}
	iv, rtt := cur.snapIv.Sub(prev.snapIv), cur.rtt.Sub(prev.rtt)
	return fmt.Sprintf("t=%4.0fs conns=%3d in=%6.0f/s out=%6.0f/s rx=%7.1fKB/s rx/client=%5.1fKB/s snap/s=%5.0f iv_p50=%dms iv_p99=%dms iv>50ms=%d gaps=%d rtt_p50=%dms rtt_p99=%dms disc=%d",
		cur.At.Seconds(), cur.conns, rate(cur.msgsIn, prev.msgsIn), rate(cur.msgsOut, prev.msgsOut),
		rate(cur.bytesIn, prev.bytesIn)/1024, perClient, rate(cur.snaps, prev.snaps),
		iv.Quantile(0.5), iv.Quantile(0.99), iv.Above(50), cur.gaps-prev.gaps,
		rtt.Quantile(0.5), rtt.Quantile(0.99), cur.disc)
}

// summary is the whole run between the steady-state window's samples
// (from: ramp done, to: end) plus the handshakes and disconnect reasons.
func Summary(from, to Sample, hs Counts, reasons map[string]int) string {
	var b strings.Builder
	dt := (to.At - from.At).Seconds()
	if dt <= 0 {
		dt = 1
	}
	iv := to.snapIv.Sub(from.snapIv)
	rtt := to.rtt.Sub(from.rtt)
	in := float64(to.bytesIn-from.bytesIn) / dt
	fmt.Fprintf(&b, "steady window %.0fs: conns=%d rx=%.1fKB/s (%.2f Mbit/s) rx/client=%.1fKB/s tx=%.1fKB/s msgs_in=%.0f/s msgs_out=%.0f/s\n",
		dt, to.conns, in/1024, in*8/1e6, PerClientKB(in, to.conns), float64(to.bytesOut-from.bytesOut)/dt/1024,
		float64(to.msgsIn-from.msgsIn)/dt, float64(to.msgsOut-from.msgsOut)/dt)
	fmt.Fprintf(&b, "snap interval: n=%d p50=%dms p90=%dms p99=%dms p99.9=%dms max<=%dms >50ms=%d >100ms=%d gaps=%d\n",
		iv.Total(), iv.Quantile(0.5), iv.Quantile(0.9), iv.Quantile(0.99), iv.Quantile(0.999), iv.Quantile(1),
		iv.Above(50), iv.Above(100), to.gaps-from.gaps)
	fmt.Fprintf(&b, "ping rtt: n=%d p50=%dms p99=%dms max<=%dms\n", rtt.Total(), rtt.Quantile(0.5), rtt.Quantile(0.99), rtt.Quantile(1))
	fmt.Fprintf(&b, "handshake: n=%d p50=%dms p99=%dms max<=%dms\n", hs.Total(), hs.Quantile(0.5), hs.Quantile(0.99), hs.Quantile(1))
	b.WriteString("disconnects:")
	if len(reasons) == 0 {
		b.WriteString(" none")
	}
	for _, k := range slices.Sorted(maps.Keys(reasons)) {
		fmt.Fprintf(&b, " %s=%d", k, reasons[k])
	}
	return b.String()
}

func PerClientKB(bytesPerSec float64, conns int64) float64 {
	if conns <= 0 {
		return 0
	}
	return bytesPerSec / float64(conns) / 1024
}

// reason names why a player's socket ended: the server's error message if
// it sent one, else the close status or the kind of read error.
func Reason(err error, serverMsg string) string {
	switch {
	case serverMsg != "":
		return "server:" + serverMsg
	case err == nil:
		return "closed"
	}
	if c := websocket.CloseStatus(err); c != -1 {
		return "close:" + c.String()
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	}
	msg := err.Error()
	if len(msg) > 60 {
		msg = msg[:60]
	}
	return "error:" + msg
}
