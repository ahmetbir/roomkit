package room

const (
	queueCap  = 8 // inputs buffered per player
	queueKeep = 4 // backlog above this is dropped at tick time
)

// queue is one seat's inputs, owned by the room goroutine. Duplicates and
// out-of-order seqs are dropped; a full queue loses its oldest entry; at
// tick time a backlog beyond queueKeep is dropped. Dropped one-shot presses
// latch onto the oldest kept input. Starved, it repeats the last input
// without its presses.
type queue[In Input[In]] struct {
	items   []In
	seqs    []uint32
	last    In
	lastSeq uint32 // highest seq accepted
	ack     uint32 // seq of the input last applied
}

func newQueue[In Input[In]]() queue[In] {
	return queue[In]{items: make([]In, 0, queueCap), seqs: make([]uint32, 0, queueCap)}
}

// started reports whether any input arrived; before that the game keeps the
// seat's own controls.
func (q *queue[In]) started() bool { return q.lastSeq > 0 }

func (q *queue[In]) push(seq uint32, in In) {
	if seq <= q.lastSeq {
		return
	}
	q.lastSeq = seq
	if len(q.items) == queueCap {
		q.drop(1)
	}
	q.items = append(q.items, in)
	q.seqs = append(q.seqs, seq)
}

// next pops this tick's input; false when starved (the held repeat).
func (q *queue[In]) next() (In, bool) {
	if len(q.items) == 0 {
		return q.last, false
	}
	if n := len(q.items) - queueKeep; n > 0 {
		q.drop(n)
	}
	in := q.items[0]
	q.ack = q.seqs[0]
	q.items = append(q.items[:0], q.items[1:]...)
	q.seqs = append(q.seqs[:0], q.seqs[1:]...)
	q.last = in.Held()
	return in, true
}

// drop removes the n oldest inputs; their presses latch onto the next one.
func (q *queue[In]) drop(n int) {
	for _, in := range q.items[:n] {
		q.items[n] = q.items[n].Latch(in)
	}
	q.items = append(q.items[:0], q.items[n:]...)
	q.seqs = append(q.seqs[:0], q.seqs[n:]...)
}
