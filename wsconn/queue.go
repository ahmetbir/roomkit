package wsconn

import "time"

// Replaceable marks messages a newer one supersedes (snapshots). When the
// outbound queue is full, the oldest queued Replaceable is evicted to make
// room; other messages are never evicted.
type Replaceable interface{ Replaceable() }

// Carrier is a Replaceable whose eviction must not lose everything (e.g. a
// snapshot's events). Carry returns the receiver extended with what older,
// an evicted message of the same kind, still has to deliver.
type Carrier interface {
	Replaceable
	Carry(older any) any
}

type outMsg struct {
	v    any
	at   time.Time
	last bool // close the connection after writing this one
}

// outQueue is a bounded FIFO; it is not synchronized (Conn guards it).
type outQueue struct {
	msgs  []outMsg
	cap   int
	carry any // an evicted Carrier no queued Replaceable has taken over yet
}

func newOutQueue(n int) *outQueue {
	return &outQueue{msgs: make([]outMsg, 0, n), cap: n}
}

func (q *outQueue) full() bool { return len(q.msgs) >= q.cap }

// push appends m. On a full queue it evicts the oldest Replaceable to make
// room and reports false only when nothing could be evicted (m dropped).
// What an evicted (or dropped) Carrier must still deliver moves to the next
// Replaceable: a later queued one, or the next one pushed.
func (q *outQueue) push(m outMsg) bool {
	if q.full() {
		i := q.oldestReplaceable()
		if i < 0 {
			if replaceable(m) {
				q.stash(m.v)
			}
			return false
		}
		old := q.msgs[i].v
		q.msgs = append(q.msgs[:i], q.msgs[i+1:]...)
		if j := q.nextReplaceable(i); j >= 0 {
			q.msgs[j].v = carry(q.msgs[j].v, old)
		} else {
			q.stash(old)
		}
	}
	if q.carry != nil && replaceable(m) {
		m.v = carry(m.v, q.carry)
		q.carry = nil
	}
	q.msgs = append(q.msgs, m)
	return true
}

// stash keeps v's carried content for the next Replaceable pushed.
func (q *outQueue) stash(v any) {
	if q.carry != nil {
		v = carry(v, q.carry)
	}
	if _, ok := v.(Carrier); ok {
		q.carry = v
	}
}

// carry returns newer extended with older's content when newer is a Carrier.
func carry(newer, older any) any {
	if c, ok := newer.(Carrier); ok {
		return c.Carry(older)
	}
	return newer
}

func replaceable(m outMsg) bool {
	_, ok := m.v.(Replaceable)
	return ok && !m.last
}

func (q *outQueue) nextReplaceable(from int) int {
	for i := from; i < len(q.msgs); i++ {
		if replaceable(q.msgs[i]) {
			return i
		}
	}
	return -1
}

func (q *outQueue) oldestReplaceable() int { return q.nextReplaceable(0) }

func (q *outQueue) pop() (outMsg, bool) {
	if len(q.msgs) == 0 {
		return outMsg{}, false
	}
	m := q.msgs[0]
	n := copy(q.msgs, q.msgs[1:])
	q.msgs[n] = outMsg{} // drop the reference for GC
	q.msgs = q.msgs[:n]
	return m, true
}
