// Package ledger keeps per-key records on disk without a database: an
// append-only JSON-lines journal and a periodic snapshot, owned by one
// goroutine. It is generic over a delta D (one thing to record) and a record
// R (what accumulates per key); a Schema folds the first into the second.
package ledger

import (
	"bufio"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Schema is the game-specific part: what a delta is keyed by and how it adds
// to a record. Fold and Key must be deterministic: the journal is replayed
// through them after a crash.
type Schema[D, R any] interface {
	Key(d D) string                // record key (pilot.Hash); "" = not recorded
	Fold(r R, d D, at time.Time) R // r is the zero value for a new key
	Empty(d D) bool                // nothing to record
}

// Keeper is an optional Schema extension: records that Keep reports true
// outlive the others at the key cap (they still age out at MaxIdle).
type Keeper[R any] interface {
	Keep(r R) bool
}

// Entry is a stored record and the last time its key was recorded.
type Entry[R any] struct {
	R    R         `json:"r"`
	Seen time.Time `json:"seen"`
}

type Options struct {
	Now          func() time.Time // default time.Now
	MaxKeys      int              // default 20 000 (memory budget)
	MaxIdle      time.Duration    // default 180 days
	CompactEvery time.Duration    // default 10 min
	MaxJournal   int64            // default 8 << 20
	Buffer       int              // default 1024
	Log          *slog.Logger     // default slog.Default()
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MaxKeys <= 0 {
		o.MaxKeys = 20_000
	}
	if o.MaxIdle <= 0 {
		o.MaxIdle = 180 * 24 * time.Hour
	}
	if o.CompactEvery <= 0 {
		o.CompactEvery = 10 * time.Minute
	}
	if o.MaxJournal <= 0 {
		o.MaxJournal = 8 << 20
	}
	if o.Buffer <= 0 {
		o.Buffer = 1024
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return o
}

// Store is an actor: every field below `closed` belongs to run().
type Store[D, R any] struct {
	dir     string
	sch     Schema[D, R]
	o       Options
	in      chan any // rec[D] | call[D, R]
	done    chan struct{}
	dropped atomic.Uint64

	// mu fences Record against Close: once closed is set, no Record can
	// enqueue, so every accepted record sits ahead of Close's message.
	mu     sync.RWMutex
	closed bool

	lock        *os.File // lockDir's flock, released after the actor stops
	entries     map[string]Entry[R]
	seq         uint64
	journal     *os.File
	w           *bufio.Writer
	size        int64
	compactedAt time.Time
	failedAt    time.Time // last failed compaction; zero when healthy
	attempts    int       // compaction attempts (tests)
	failing     bool      // journal writes are failing; logged once per episode
	torn        bool      // the journal may end inside a partial line
}

// rec is a delta stamped with the caller's clock at Record time.
type rec[D any] struct {
	d  D
	at time.Time
}

// call runs f inside the actor; stop ends the actor after the reply.
type call[D, R any] struct {
	f     func(*Store[D, R]) (stop bool, err error)
	reply chan error
}

// Open locks dir (ErrLocked while another store has it), loads the snapshot
// and the journal under it (0700, files 0600) and starts the actor.
func Open[D, R any](dir string, sch Schema[D, R], o Options) (*Store[D, R], error) {
	o = o.withDefaults()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700) // best effort: MkdirAll keeps an existing dir's mode
	lock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	s := &Store[D, R]{dir: dir, sch: sch, o: o, in: make(chan any, o.Buffer), done: make(chan struct{}), entries: map[string]Entry[R]{}, lock: lock}
	if err := s.open(); err != nil {
		lock.Close()
		return nil, err
	}
	go s.run()
	return s, nil
}

// open loads the files and opens the journal for appending.
func (s *Store[D, R]) open() error {
	if err := s.load(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, journalName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	s.journal, s.w, s.size, s.compactedAt = f, bufio.NewWriter(f), st.Size(), s.o.Now()
	if err := s.endLine(); err != nil { // a torn last line must not swallow the next record
		f.Close()
		return err
	}
	return nil
}

// Record queues d; it never blocks. false means d was dropped (counted).
func (s *Store[D, R]) Record(d D) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		s.dropped.Add(1)
		return false
	}
	now := time.Now
	if s.o.Now != nil {
		now = s.o.Now
	}
	select {
	case s.in <- rec[D]{d, now().Truncate(time.Millisecond).UTC()}:
		return true
	default:
		s.dropped.Add(1)
		return false
	}
}

func (s *Store[D, R]) Dropped() uint64 { return s.dropped.Load() }

// View runs f inside the actor with every entry and waits for it. f must
// treat the map as read-only and must not keep it. After Close, f is not run.
func (s *Store[D, R]) View(f func(all map[string]Entry[R])) {
	s.call(func(s *Store[D, R]) (bool, error) { f(s.entries); return false, nil })
}

// Get returns the record under key and when it was last recorded.
func (s *Store[D, R]) Get(key string) (R, time.Time, bool) {
	var e Entry[R]
	var ok bool
	s.call(func(s *Store[D, R]) (bool, error) { e, ok = s.entries[key]; return false, nil })
	return e.R, e.Seen, ok
}

// Close applies every record accepted before it, writes a final snapshot and
// stops the actor; later Records are dropped and counted, later Closes return nil.
func (s *Store[D, R]) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	err := s.call(func(s *Store[D, R]) (bool, error) {
		err := s.compact(s.o.Now())
		return true, errors.Join(err, s.journal.Close())
	})
	<-s.done // the lock is free once Close returns
	return err
}

func (s *Store[D, R]) compactNow() {
	s.call(func(s *Store[D, R]) (bool, error) { return false, s.compact(s.o.Now()) })
}

// snapshotOnly writes the snapshot without truncating the journal (tests).
func (s *Store[D, R]) snapshotOnly() {
	s.call(func(s *Store[D, R]) (bool, error) { return false, s.writeSnapshot() })
}

// crash flushes the journal and stops without a snapshot (tests: power loss).
func (s *Store[D, R]) crash() {
	s.call(func(s *Store[D, R]) (bool, error) { s.w.Flush(); s.journal.Close(); return true, nil })
	<-s.done
}

// call runs f in the actor and waits. After the actor has stopped it
// returns nil without running f.
func (s *Store[D, R]) call(f func(*Store[D, R]) (bool, error)) error {
	r := make(chan error, 1)
	select {
	case s.in <- call[D, R]{f, r}:
	case <-s.done:
		return nil
	}
	select {
	case err := <-r:
		return err
	case <-s.done:
		select { // the reply is sent before done closes
		case err := <-r:
			return err
		default:
			return nil
		}
	}
}

func (s *Store[D, R]) run() {
	defer close(s.done)
	defer s.lock.Close() // flock released: the next store may open the dir
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case m := <-s.in:
			switch m := m.(type) {
			case rec[D]:
				s.record(m.d, m.at)
			case call[D, R]:
				stop, err := m.f(s)
				m.reply <- err
				if stop {
					return
				}
			}
		case <-tick.C:
			now := s.o.Now()
			if s.size > 0 && (now.Sub(s.compactedAt) >= s.o.CompactEvery || s.size > s.o.MaxJournal) {
				s.tryCompact(now)
			}
		}
	}
}

// tryCompact compacts unless a compaction failed less than a minute ago.
func (s *Store[D, R]) tryCompact(now time.Time) {
	if !s.failedAt.IsZero() && now.Sub(s.failedAt) < time.Minute {
		return
	}
	s.attempts++
	if err := s.compact(now); err != nil {
		s.failedAt = now
		s.o.Log.Error("ledger: compaction failed", "err", err) // never logs keys or records
		return
	}
	s.failedAt = time.Time{}
}
