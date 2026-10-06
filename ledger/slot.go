package ledger

import (
	"sync"
	"sync/atomic"
	"time"
)

// slotQueue bounds the deltas a waiting Slot holds. It stays under the
// store's default Buffer, so replaying the queue into a fresh store never
// overflows it.
const slotQueue = 512

type slotState int

const (
	slotWaiting slotState = iota // no store yet: deltas queue
	slotOpen                     // deltas go to the store
	slotClosed                   // drained: deltas are dropped
)

// Slot is where rooms record and the API reads while the store may not be
// open yet (it waits for the previous server's lock during a blue/green
// handoff) or has been handed off (drain). It is safe for concurrent use.
type Slot[D, R any] struct {
	mu      sync.Mutex
	state   slotState
	st      *Store[D, R]
	queue   []D
	dropped atomic.Uint64 // deltas lost by the slot itself (full queue, closed)
}

// NewSlot returns a waiting slot.
func NewSlot[D, R any]() *Slot[D, R] { return &Slot[D, R]{} }

// Set hands the slot its store and replays the queued deltas into it. On a
// closed slot it closes st (releasing its lock) and returns false.
func (s *Slot[D, R]) Set(st *Store[D, R]) bool {
	s.mu.Lock()
	if s.state != slotWaiting {
		s.mu.Unlock()
		st.Close()
		return false
	}
	for _, d := range s.queue {
		st.Record(d) // never blocks; len(queue) < the store's buffer
	}
	s.queue, s.st, s.state = nil, st, slotOpen
	s.mu.Unlock()
	return true
}

// Close closes the store (final snapshot, lock released) and drops every
// later delta until Reopen. Queued deltas of a waiting slot are dropped.
func (s *Slot[D, R]) Close() error {
	s.mu.Lock()
	st := s.st
	s.dropped.Add(uint64(len(s.queue)))
	s.st, s.queue, s.state = nil, nil, slotClosed
	s.mu.Unlock()
	if st == nil {
		return nil
	}
	s.dropped.Add(st.Dropped()) // the closed store's losses stay counted
	return st.Close()
}

// Reopen makes a closed slot wait for a store again (undrain).
func (s *Slot[D, R]) Reopen() {
	s.mu.Lock()
	if s.state == slotClosed {
		s.state = slotWaiting
	}
	s.mu.Unlock()
}

// Ready reports whether a store is open behind the slot.
func (s *Slot[D, R]) Ready() bool { return s.store() != nil }

func (s *Slot[D, R]) store() *Store[D, R] {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// Record forwards d to the store, queues it while waiting, or drops it
// (counted). It never blocks.
func (s *Slot[D, R]) Record(d D) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.state == slotOpen:
		return s.st.Record(d)
	case s.state == slotWaiting && len(s.queue) < slotQueue:
		s.queue = append(s.queue, d)
		return true
	}
	s.dropped.Add(1)
	return false
}

// Dropped counts deltas lost by the slot and by its open store.
func (s *Slot[D, R]) Dropped() uint64 {
	n := s.dropped.Load()
	if st := s.store(); st != nil {
		n += st.Dropped()
	}
	return n
}

// Get is Store.Get; not found with no open store.
func (s *Slot[D, R]) Get(key string) (R, time.Time, bool) {
	if st := s.store(); st != nil {
		return st.Get(key)
	}
	var zero R
	return zero, time.Time{}, false
}

// View is Store.View; f is not run with no open store.
func (s *Slot[D, R]) View(f func(all map[string]Entry[R])) {
	if st := s.store(); st != nil {
		st.View(f)
	}
}
