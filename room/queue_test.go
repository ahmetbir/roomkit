package room

import "testing"

// in is a test input: X is a held control, Shot and Alt one-shot presses.
type in struct {
	X         int
	Shot, Alt bool
}

func (i in) Latch(d in) in { i.Shot, i.Alt = i.Shot || d.Shot, i.Alt || d.Alt; return i }
func (i in) Held() in      { i.Shot, i.Alt = false, false; return i }

func TestQueueOrderDuplicatesAndCap(t *testing.T) {
	q := newQueue[in]()
	q.push(2, in{X: 2})
	q.push(1, in{X: 1}) // out of order: dropped
	q.push(2, in{X: 9}) // duplicate: dropped
	q.push(3, in{X: 3})
	if v, ok := q.next(); !ok || v.X != 2 || q.ack != 2 {
		t.Fatalf("%+v %v ack=%d", v, ok, q.ack)
	}
	for s := uint32(4); s < 4+queueCap+2; s++ {
		q.push(s, in{X: int(s)})
	}
	if len(q.items) != queueCap {
		t.Fatalf("cap: %d", len(q.items))
	}
}

func TestQueueBacklogKeepsPressesAndStarvedRepeatsHeld(t *testing.T) {
	q := newQueue[in]()
	if q.started() {
		t.Fatal("started before any input")
	}
	q.push(1, in{X: 1, Shot: true})
	for s := uint32(2); s <= 7; s++ {
		q.push(s, in{X: int(s)})
	}
	v, ok := q.next() // backlog 7 > keep 4: seq 1..3 dropped, their press kept
	if !ok || v.X != 4 || !v.Shot || q.ack != 4 {
		t.Fatalf("%+v ack=%d", v, q.ack)
	}
	for range 3 {
		q.next()
	}
	r, ok := q.next() // starved: repeat the last without its press
	if ok || r.X != 7 || r.Shot || !q.started() {
		t.Fatalf("repeat %+v %v", r, ok)
	}
}

// Moved from the Dogfight session tests (TestSessionQueue): nine queued
// inputs and a stale one; the tick trims to the newest four, then the queue
// drains and repeats the last.
func TestQueueBacklogAckAndRepeat(t *testing.T) {
	q := newQueue[in]()
	if _, ok := q.next(); ok {
		t.Fatal("empty queue produced a fresh input")
	}
	for seq := uint32(1); seq <= 9; seq++ {
		q.push(seq, in{X: int(seq)})
	}
	q.push(2, in{X: 10}) // out of order
	v, ok := q.next()
	if !ok || q.ack != 6 || v.X != 6 {
		t.Fatalf("after backlog: ack=%d x=%d", q.ack, v.X)
	}
	for range 3 {
		q.next()
	}
	if q.ack != 9 {
		t.Fatalf("ack=%d", q.ack)
	}
	v, ok = q.next()
	if ok || v.X != 9 || q.ack != 9 {
		t.Fatalf("repeat-last: ok=%v x=%d ack=%d", ok, v.X, q.ack)
	}
}

// Moved from TestSessionBacklogLatchesPresses: presses dropped from a
// backlog (queue overflow or trim) ride on the next kept input, once.
func TestQueueOverflowAndTrimLatchPresses(t *testing.T) {
	q := newQueue[in]()
	for seq := uint32(1); seq <= 9; seq++ {
		q.push(seq, in{X: int(seq), Shot: seq == 1, Alt: seq == 3}) // 1 lost to overflow, 3 to the trim
	}
	v, ok := q.next()
	if !ok || q.ack != 6 || !v.Shot || !v.Alt {
		t.Fatalf("kept input: ack=%d %+v", q.ack, v)
	}
	if v, _ = q.next(); v.Shot || v.Alt {
		t.Fatalf("presses repeated: %+v", v)
	}
}

// Moved from TestRepeatedInputDropsPresses and TestBacklogKeepsBomb: a
// starved queue repeats the last input's held controls, never its presses.
func TestStarvedRepeatDropsPresses(t *testing.T) {
	q := newQueue[in]()
	q.push(1, in{X: 7, Shot: true, Alt: true})
	got, ok := q.next()
	if !ok || !got.Shot || !got.Alt {
		t.Fatalf("fresh input lost presses: %+v", got)
	}
	got, ok = q.next()
	if ok || got.Shot || got.Alt || got.X != 7 {
		t.Fatalf("repeated input: ok=%v %+v", ok, got)
	}
}
