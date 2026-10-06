package wsconn

import (
	"slices"
	"testing"
)

// evSnap is a snapshot carrying events that must survive its eviction.
type evSnap struct {
	N  int
	Ev []string
}

func (evSnap) Replaceable() {}

func (s evSnap) Carry(older any) any {
	o := older.(evSnap)
	s.Ev = append(slices.Clone(o.Ev), s.Ev...)
	return s
}

func evs(q *outQueue) [][]string {
	var out [][]string
	for _, m := range q.msgs {
		if s, ok := m.v.(evSnap); ok {
			out = append(out, s.Ev)
		}
	}
	return out
}

func TestEvictedEventsMoveToNextQueuedSnapshot(t *testing.T) {
	q := newOutQueue(3)
	for _, v := range []any{evSnap{1, []string{"a"}}, players{1}, evSnap{2, []string{"b"}}} {
		q.push(outMsg{v: v})
	}
	q.push(outMsg{v: evSnap{3, []string{"c"}}}) // evicts 1: its event rides with 2
	got := evs(q)
	if len(got) != 2 || !slices.Equal(got[0], []string{"a", "b"}) || !slices.Equal(got[1], []string{"c"}) {
		t.Fatalf("events %v", got)
	}
}

func TestEvictedEventsWaitForTheNextSnapshot(t *testing.T) {
	q := newOutQueue(2)
	q.push(outMsg{v: players{1}})
	q.push(outMsg{v: evSnap{1, []string{"a"}}})
	q.push(outMsg{v: players{2}}) // evicts 1; no later snapshot queued
	if q.push(outMsg{v: evSnap{2, []string{"b"}}}) {
		t.Fatal("full queue of non-snapshots accepted a snapshot")
	}
	q.pop()
	q.push(outMsg{v: evSnap{3, []string{"c"}}})
	if got := evs(q); len(got) != 1 || !slices.Equal(got[0], []string{"a", "b", "c"}) {
		t.Fatalf("events %v", got)
	}
}
