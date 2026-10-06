package wsconn

import (
	"sync/atomic"
	"testing"
	"time"
)

type counts struct{ out, drop atomic.Int64 }

func (c *counts) Out()  { c.out.Add(1) }
func (c *counts) Drop() { c.drop.Add(1) }

func TestCountsWrites(t *testing.T) {
	k := &counts{}
	ws := serve(t, Options{Count: k}, echo)
	roundTrip(t, ws, `{"t":"ping"}`)
	roundTrip(t, ws, `{"t":"ping"}`)
	deadline := time.Now().Add(2 * time.Second)
	for k.out.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if k.out.Load() != 2 || k.drop.Load() != 0 {
		t.Fatalf("out %d drop %d", k.out.Load(), k.drop.Load())
	}
}

// Every snapshot lost to a full queue is counted, evicted or refused; a
// refused non-snapshot is not a snapshot drop.
func TestCountsSnapshotDrops(t *testing.T) {
	k := &counts{}
	c := &Conn{o: Options{QueueSize: 2, Count: k}.withDefaults(), q: newOutQueue(2), wake: make(chan struct{}, 1), done: make(chan struct{})}
	c.Send(snap{1})
	c.Send(snap{2})
	c.Send(snap{3})    // evicts 1
	c.Send(players{1}) // evicts 2
	c.Send(players{2}) // evicts 3
	c.Send(snap{4})    // refused: nothing to evict
	c.Send(players{3}) // refused, not a snapshot
	if k.drop.Load() != 4 || k.out.Load() != 0 {
		t.Fatalf("drops %d out %d", k.drop.Load(), k.out.Load())
	}
}
