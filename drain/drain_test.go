package drain

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeSrv struct {
	mu      sync.Mutex
	on      bool
	conns   int
	flushes int
	flush   func() // FlushStats: what the rooms would record
}

func (f *fakeSrv) FlushStats(context.Context) bool {
	f.mu.Lock()
	f.flushes++
	f.mu.Unlock()
	if f.flush != nil {
		f.flush()
	}
	return true
}

func (f *fakeSrv) Drain(on bool) { f.mu.Lock(); f.on = on; f.mu.Unlock() }
func (f *fakeSrv) Conns() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.conns }
func (f *fakeSrv) set(n int)     { f.mu.Lock(); f.conns = n; f.mu.Unlock() }
func (f *fakeSrv) draining() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on
}
func (f *fakeSrv) flushCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.flushes }

// flock is the store directory's single-writer lock, shared by the old and
// the new server's store.
type flock struct {
	mu   sync.Mutex
	held bool
}

func (l *flock) try() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return false
	}
	l.held = true
	return true
}

func (l *flock) free() { l.mu.Lock(); l.held = false; l.mu.Unlock() }

// store is a fake Handoff: Acquire waits for the lock, Release frees it.
type store struct {
	lk     *flock
	mu     sync.Mutex
	open   bool
	events []string // "record" (while open), "release"
}

func (s *store) Acquire(ctx context.Context) {
	for !s.lk.try() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
	s.mu.Lock()
	s.open = true
	s.mu.Unlock()
}

func (s *store) Release() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open {
		s.open = false
		s.lk.free()
	}
	s.events = append(s.events, "release")
	return nil
}

func (s *store) Reopen() {}

func (s *store) ready() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.open }

// record is a room's tally: kept only while the store is open.
func (s *store) record() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open {
		s.events = append(s.events, "record")
	}
}

func (s *store) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

type drainRig struct {
	srv  *fakeSrv
	st   *store
	sig  chan os.Signal
	quit chan struct{}
	done chan struct{}
}

func startDrainer(t *testing.T, conns int, max time.Duration) *drainRig {
	t.Helper()
	r := &drainRig{srv: &fakeSrv{conns: conns}, st: &store{lk: &flock{}},
		sig: make(chan os.Signal, 1), quit: make(chan struct{}), done: make(chan struct{})}
	a := New(r.srv, r.st, func() { close(r.quit) }, Options{Every: 5 * time.Millisecond, Max: max})
	go func() { a.Run(t.Context(), r.sig); close(r.done) }()
	eventually(t, r.st.ready, "store open at start")
	return r
}

func eventually(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func closed(c <-chan struct{}) func() bool {
	return func() bool {
		select {
		case <-c:
			return true
		default:
			return false
		}
	}
}

// SIGUSR1: drain, hand the store's lock over at once, quit when the last
// socket closes.
func TestDrainHandsOffAndQuitsWhenEmpty(t *testing.T) {
	r := startDrainer(t, 2, time.Hour)
	r.sig <- syscall.SIGUSR1
	eventually(t, r.srv.draining, "server not draining")
	eventually(t, func() bool { return !r.st.ready() }, "store still open")
	next := &store{lk: r.st.lk} // the new server's store: it opens once the drain released the lock
	eventually(t, func() bool { return next.lk.try() }, "lock not released on drain")
	time.Sleep(30 * time.Millisecond)
	if closed(r.quit)() {
		t.Fatal("quit with players still connected")
	}
	r.srv.set(0)
	eventually(t, closed(r.quit), "no quit after the last socket closed")
	eventually(t, closed(r.done), "drainer still running")
}

func TestDrainQuitsAfterMax(t *testing.T) {
	r := startDrainer(t, 3, 40*time.Millisecond)
	r.sig <- syscall.SIGUSR1
	eventually(t, closed(r.quit), "no quit after the drain max")
}

// SIGUSR2 undoes a drain: serving again, the store reacquired (lock free again).
func TestUndrain(t *testing.T) {
	r := startDrainer(t, 1, time.Hour)
	r.sig <- syscall.SIGUSR1
	eventually(t, func() bool { return !r.st.ready() }, "store still open")
	r.sig <- syscall.SIGUSR2
	eventually(t, func() bool { return !r.srv.draining() }, "still draining")
	eventually(t, r.st.ready, "store not reacquired")
	r.srv.set(0)
	time.Sleep(30 * time.Millisecond)
	if closed(r.quit)() {
		t.Fatal("an undrained server quit")
	}
}

// The rooms' open tallies are flushed into the store before the drain
// releases it: the next server's store has them.
func TestDrainFlushesTalliesBeforeHandoff(t *testing.T) {
	r := startDrainer(t, 1, time.Hour)
	r.srv.flush = r.st.record
	r.sig <- syscall.SIGUSR1
	eventually(t, func() bool { return len(r.st.log()) == 2 }, "no handoff")
	if got := r.st.log(); got[0] != "record" || got[1] != "release" {
		t.Fatalf("flushed tally lost in the handoff: %v", got)
	}
}

// Without a store there is nothing to flush or hand over; the drain itself
// is unchanged.
func TestDrainWithoutStore(t *testing.T) {
	srv := &fakeSrv{conns: 1}
	sig, quit := make(chan os.Signal, 1), make(chan struct{})
	a := New(srv, nil, func() { close(quit) }, Options{Every: 5 * time.Millisecond, Max: time.Hour})
	go a.Run(t.Context(), sig)
	sig <- syscall.SIGUSR1
	eventually(t, srv.draining, "server not draining")
	srv.set(0)
	eventually(t, closed(quit), "no quit after the last socket closed")
	if srv.flushCount() != 0 {
		t.Fatal("flushed without a store")
	}
}
