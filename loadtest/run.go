package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Script is what a game tells the load test.
//
// Concurrency: methods are called from several goroutines at once. For one
// player, Hello and Create run on the player's goroutine, Input on its send
// goroutine and React on its read goroutine, so Input and React run
// concurrently; one player's React calls are serial. Different players run
// concurrently too. An implementation must guard any state it shares between
// them. React's replies are best-effort: one dropped while another is still
// waiting to be sent is not retried.
type Script interface {
	Hello(i int) any                            // player i's hello
	Create() any                                // a creator's create message
	Input(i int, seq uint32) any                // player i's input number seq (Config.InputHz)
	React(i, you int, t string, raw []byte) any // a reply to a server message other than snap/pong/error, or nil; raw is valid until return
}

// Config is one run: the target, the crowd, the game's cadence and the timing.
type Config struct {
	URL                           string
	Players, Rooms                int
	InputHz                       int // inputs a player sends per second (pings follow every InputHz inputs: 1 s)
	SnapEvery                     int // server ticks between two snapshots: a larger tick step is a gap
	Duration, Ramp, Settle, Every time.Duration
}

// pace is a player's share of the game's cadence.
type pace struct{ inputHz, snapEvery int }

// Run starts every player on its ramp slot and reports until the run ends.
// The game's cadence has no default: a wrong SnapEvery miscounts gaps silently.
func Run(ctx context.Context, c Config, sc Script) error {
	if c.InputHz <= 0 || c.SnapEvery <= 0 {
		return fmt.Errorf("loadtest: InputHz (%d) and SnapEvery (%d) must be > 0", c.InputHz, c.SnapEvery)
	}
	st := NewStats()
	codes := make([]*roomCode, c.Rooms)
	for i := range codes {
		codes[i] = newRoomCode()
	}
	begin := time.Now()
	rctx, cancel := context.WithDeadline(ctx, begin.Add(c.Ramp+c.Duration))
	defer cancel()
	var wg sync.WaitGroup
	for i, sl := range Assign(c.Players, c.Rooms) {
		p := &player{i: i, slot: sl, url: c.URL, sc: sc, pace: pace{c.InputHz, c.SnapEvery}, st: st, epoch: begin}
		if sl.Room >= 0 {
			p.code = codes[sl.Room]
		}
		wg.Go(func() {
			select {
			case <-time.After(time.Until(begin.Add(StartAt(i, c.Players, c.Ramp)))):
				p.run(rctx)
			case <-rctx.Done():
			}
		})
	}
	from, end := report(rctx, c, st, begin)
	wg.Wait()
	fmt.Println(Summary(from, end, st.Handshake.Snapshot(), st.ReasonCounts()))
	return nil
}

// report prints a line every c.Every until ctx ends; it returns the
// samples that bound the steady-state window (ramp + settle to the end).
func report(ctx context.Context, c Config, st *Stats, begin time.Time) (from, end Sample) {
	t := time.NewTicker(c.Every)
	defer t.Stop()
	prev := st.Sample(0)
	steady := false
	for {
		select {
		case <-ctx.Done():
			// The last periodic sample ends the window: at the deadline
			// players are already closing, which would skew per-client rates.
			if !steady {
				from = Sample{} // never steady: the whole run
			}
			end = prev
			if end.At <= from.At {
				end = st.Sample(time.Since(begin))
			}
			return from, end
		case <-t.C:
			cur := st.Sample(time.Since(begin))
			fmt.Println(Line(prev, cur))
			if !steady && cur.At >= c.Ramp+c.Settle {
				from, steady = cur, true
			}
			prev = cur
		}
	}
}
