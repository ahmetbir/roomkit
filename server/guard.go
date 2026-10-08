package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/ahmetbir/roomkit/limit"
	"github.com/ahmetbir/roomkit/netproto"
)

// msgGuard rate-limits one connection's inbound messages; owned by that
// connection's handler goroutine. Inputs have their own bucket and are only
// ever dropped: a network stall delivers seconds of 60 Hz input at once, and
// the room keeps just the newest anyway. Every other kind shares the all
// bucket and may have its own; refusing one of those ends the connection.
type msgGuard struct {
	now                 func() time.Time
	in, all, pick, ping limit.Bucket
	ceil                ceiling
}

func newMsgGuard(l Limits, now func() time.Time) *msgGuard {
	return &msgGuard{
		now:  now,
		in:   limit.NewBucket(l.MsgRate, l.MsgBurst),
		all:  limit.NewBucket(l.MsgRate, l.MsgBurst),
		pick: limit.NewBucket(l.PickRate, l.PickBurst),
		ping: limit.NewBucket(l.PingRate, l.PingBurst),
	}
}

// verdict is what the guard does with one message.
type verdict int

const (
	pass verdict = iota
	drop         // ignore the message, keep the connection
	kick         // end the connection
)

// frame counts one raw frame of n bytes against the hard ceiling (before
// decoding); a refusal names the limit.
func (g *msgGuard) frame(n int) (verdict, string) {
	if lim := g.ceil.add(g.now(), n); lim != "" {
		return kick, lim
	}
	return pass, ""
}

// check admits a decoded message of type t and rate class c; a refusal
// names its bucket. The core types come first: inputs have their own
// bucket, pings theirs; c only adds the choice bucket to a game type, and
// every type but the input is charged to the shared bucket after it.
func (g *msgGuard) check(t string, c Class) (verdict, string) {
	now := g.now()
	switch {
	case t == netproto.TIn:
		if !g.in.Allow(now) {
			return drop, "in"
		}
		return pass, ""
	case t == netproto.TPing:
		if !g.ping.Allow(now) {
			return kick, "ping"
		}
	case c == ClassChoice:
		if !g.pick.Allow(now) {
			return kick, "pick"
		}
	}
	if !g.all.Allow(now) {
		return kick, "all"
	}
	return pass, ""
}

const rejectLogEvery = 10 * time.Second

// rejectLog logs refused connections and clients without letting a flood
// flood the log: per reason, at most one line per rejectLogEvery, carrying
// the number of rejections since the previous line and the latest address.
// attrs (e.g. the bucket and message type of a flood) come from the latest
// refusal. onReject sees every refusal, logged or not.
type rejectLog struct {
	now      func() time.Time
	onReject func(reason string)
	mu       sync.Mutex
	count    map[string]int
	last     map[string]time.Time
}

func newRejectLog(now func() time.Time, onReject func(reason string)) *rejectLog {
	return &rejectLog{now: now, onReject: onReject, count: map[string]int{}, last: map[string]time.Time{}}
}

func (l *rejectLog) note(reason, ip string, attrs ...any) { l.noteKey(reason, reason, ip, attrs...) }

// noteKey is note with the log line throttled and counted per key instead
// of per reason (the metric still counts reason): one line per refusal
// code, say. Keys must come from a small fixed set.
func (l *rejectLog) noteKey(reason, key, ip string, attrs ...any) {
	if l.onReject != nil {
		l.onReject(reason)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count[key]++
	now := l.now()
	if now.Sub(l.last[key]) < rejectLogEvery {
		return
	}
	slog.Info("rejected", append([]any{"reason", reason, "ip", ip, "count", l.count[key]}, attrs...)...)
	l.count[key] = 0
	l.last[key] = now
}
