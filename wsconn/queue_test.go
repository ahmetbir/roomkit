package wsconn

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

type snap struct{ N int }

func (snap) Replaceable() {}

type players struct{ N int }

func contents(q *outQueue) []any {
	var out []any
	for _, m := range q.msgs {
		out = append(out, m.v)
	}
	return out
}

func TestQueueEvictsOldestSnapshot(t *testing.T) {
	q := newOutQueue(3)
	for _, v := range []any{snap{1}, snap{2}, snap{3}} {
		q.push(outMsg{v: v})
	}
	if !q.push(outMsg{v: snap{4}}) {
		t.Fatal("snapshot dropped instead of evicting")
	}
	if got, want := contents(q), []any{snap{2}, snap{3}, snap{4}}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestQueueNeverEvictsOthers(t *testing.T) {
	q := newOutQueue(3)
	for _, v := range []any{players{1}, snap{1}, snap{2}} {
		q.push(outMsg{v: v})
	}
	q.push(outMsg{v: snap{3}})    // evicts snap 1
	q.push(outMsg{v: players{2}}) // evicts snap 2
	q.push(outMsg{v: snap{4}})    // evicts snap 3
	if got, want := contents(q), []any{players{1}, players{2}, snap{4}}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	q.push(outMsg{v: players{3}}) // evicts snap 4
	if q.push(outMsg{v: snap{5}}) {
		t.Fatal("queue full of non-snapshots accepted a message")
	}
	if got, want := contents(q), []any{players{1}, players{2}, players{3}}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if m, _ := q.pop(); m.v != (players{1}) || len(q.msgs) != 2 {
		t.Fatalf("pop: %v", m.v)
	}
}

// TestFullQueueDeliversRosterAndNewestSnap fills a lagging connection's
// queue with snapshots around one roster message.
func TestFullQueueDeliversRosterAndNewestSnap(t *testing.T) {
	type msg struct {
		T string
		N int
	}
	ws := serve(t, Options{QueueSize: 4, Lag: 100 * time.Millisecond}, func(c *Conn) {
		for i := 1; i <= 20; i++ {
			c.Send(snapMsg{T: "snap", N: i})
			if i == 3 {
				c.Send(map[string]any{"T": "players", "N": 0})
			}
		}
		for range c.Recv() {
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var snaps []int
	sawPlayers := false
	for len(snaps) == 0 || snaps[len(snaps)-1] != 20 {
		_, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (snaps %v)", err, snaps)
		}
		var m msg
		json.Unmarshal(b, &m)
		if m.T == "players" {
			sawPlayers = true
		} else {
			snaps = append(snaps, m.N)
		}
	}
	if !sawPlayers {
		t.Fatal("players message evicted")
	}
	if !slices.IsSorted(snaps) || len(snaps) > 5 {
		t.Fatalf("snaps %v: want ordered, oldest evicted", snaps)
	}
}

type snapMsg struct {
	T string
	N int
}

func (snapMsg) Replaceable() {}
