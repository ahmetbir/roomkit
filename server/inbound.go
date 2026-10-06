package server

import (
	"log/slog"
	"time"
)

// Hard ceiling on one connection's inbound traffic, far above any stall
// burst (a 5 s stall releases ~300 inputs once, not sustained): more than
// 300 messages or 64 KB per second averaged over a 5 s window ends it.
const (
	ceilWindow = 5 * time.Second
	ceilMsgs   = 300 * 5
	ceilBytes  = 64 << 10 * 5
)

// ceiling counts frames and bytes in fixed ceilWindow windows.
type ceiling struct {
	start       time.Time
	msgs, bytes int
}

// add counts one frame of n bytes and names the limit it broke, "" if none.
func (c *ceiling) add(now time.Time, n int) string {
	if now.Sub(c.start) >= ceilWindow {
		*c = ceiling{start: now}
	}
	c.msgs++
	c.bytes += n
	switch {
	case c.msgs > ceilMsgs:
		return "ceiling-msgs"
	case c.bytes > ceilBytes:
		return "ceiling-bytes"
	}
	return ""
}

// dropLog counts one connection's dropped inputs and logs them at most once
// per rejectLogEvery while it lasts, and once more at its end.
type dropLog struct {
	at           time.Time
	since, total int
}

func (d *dropLog) note(now time.Time, ip string) {
	d.since++
	d.total++
	if now.Sub(d.at) >= rejectLogEvery {
		d.flush(ip)
		d.at = now
	}
}

// flush logs the drops since the last line, if any.
func (d *dropLog) flush(ip string) {
	if d.since == 0 {
		return
	}
	slog.Info("inputs dropped", "ip", ip, "count", d.since, "total", d.total)
	d.since = 0
}
