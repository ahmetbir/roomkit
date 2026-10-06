package room

import "context"

// FlushStats hands the game's open tallies to its stats sink
// (Game.FlushStats) while the players stay seated. A draining server calls
// it before it closes its stats store (blue/green deploy), so in-progress
// sessions are not lost. It reports whether the room acknowledged before
// ctx ended; a stopped room has already flushed everything (closeAll) and
// reports true.
func (r *Room[M, In, X]) FlushStats(ctx context.Context) bool {
	ack := make(chan struct{})
	select {
	case r.flushes <- ack:
	case <-r.done:
		return true
	case <-ctx.Done():
		return false
	}
	select {
	case <-ack:
		return true
	case <-ctx.Done():
		return false
	}
}
