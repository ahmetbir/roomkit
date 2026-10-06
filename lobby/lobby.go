// Package lobby creates rooms under short join codes and forgets them when
// they finish.
package lobby

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
)

// codeAlphabet omits I, O, 0 and 1. Its 32 letters divide 256, so a random
// byte maps to a letter without bias.
const (
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLen      = 4
	codeTries    = 16
)

var (
	ErrNoCode = errors.New("lobby: no free room code")
	ErrBusy   = errors.New("lobby: room limit reached")
	// ErrDraining: the server is handing over to a new version (blue/green
	// deploy); it starts no room.
	ErrDraining = errors.New("lobby: draining")
)

// Options configure a lobby.
type Options[S any, M room.Msg[In], In room.Input[In], X any] struct {
	MaxRooms int               // rooms running at once; 0 = no limit
	Metrics  *metrics.Registry // nil = not measured
	Room     room.Options      // template; Seq and Metrics are set per room
	New      func(s S) (room.Game[M, In, X], error)
}

type Lobby[S any, M room.Msg[In], In room.Input[In], X any] struct {
	ctx      context.Context
	maxRooms int
	m        *metrics.Registry
	ro       room.Options
	newGame  func(S) (room.Game[M, In, X], error)
	running  sync.WaitGroup
	mu       sync.Mutex
	rooms    map[string]*room.Room[M, In, X]
	seq      int64 // rooms created so far; a room's Seq is its rank
	closed   bool  // Wait has begun: no room may start (running.Add would race Wait)
	draining bool  // Drain(true): no room starts, quick play picks none
}

// Drain(true) refuses new rooms (ErrDraining) and quick-play picks while
// running rooms go on; Drain(false) undoes it.
func (l *Lobby[S, M, In, X]) Drain(on bool) {
	l.mu.Lock()
	l.draining = on
	l.mu.Unlock()
}

// New returns a lobby whose rooms stop when ctx is cancelled.
func New[S any, M room.Msg[In], In room.Input[In], X any](ctx context.Context, o Options[S, M, In, X]) *Lobby[S, M, In, X] {
	return &Lobby[S, M, In, X]{ctx: ctx, maxRooms: o.MaxRooms, m: o.Metrics, ro: o.Room, newGame: o.New,
		rooms: map[string]*room.Room[M, In, X]{}}
}

// Create starts a room under a fresh code. It removes itself when done.
// Past maxRooms, or once the lobby is shutting down, it fails with ErrBusy.
func (l *Lobby[S, M, In, X]) Create(s S) (*room.Room[M, In, X], error) {
	l.mu.Lock()
	if l.draining {
		l.mu.Unlock()
		return nil, ErrDraining
	}
	if l.closed || l.ctx.Err() != nil || (l.maxRooms > 0 && len(l.rooms) >= l.maxRooms) {
		l.mu.Unlock()
		return nil, ErrBusy
	}
	code, err := l.freeCode()
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	l.rooms[code] = nil // reserve the code; building the game takes a while
	l.running.Add(1)    // under l.mu, so never after Wait has begun
	l.seq++
	seq := l.seq
	l.mu.Unlock()

	r, err := l.build(code, s, seq)
	if err != nil {
		l.running.Done() // the room never runs: Wait must not hang on it
		return nil, err
	}
	l.mu.Lock()
	l.rooms[code] = r
	l.mu.Unlock()

	slog.Info("room created", "code", code, "mode", r.Label())
	l.roomsGauge(1)
	go func() {
		defer l.running.Done()
		r.Run(l.ctx)
		l.roomsGauge(-1)
		l.mu.Lock()
		if l.rooms[code] == r {
			delete(l.rooms, code)
		}
		l.mu.Unlock()
		slog.Info("room closed", "code", code, "mode", r.Label())
	}()
	return r, nil
}

func (l *Lobby[S, M, In, X]) roomsGauge(d int64) {
	if l.m != nil {
		l.m.Rooms.Add(d)
	}
}

// FlushStats asks every running room to hand its open tallies to the stats
// sink (room.FlushStats), all at once, and waits for them until ctx ends.
// It reports whether every room acknowledged.
func (l *Lobby[S, M, In, X]) FlushStats(ctx context.Context) bool {
	rooms := l.live()
	acks := make(chan bool, len(rooms))
	for _, r := range rooms {
		go func() { acks <- r.FlushStats(ctx) }()
	}
	ok := true
	for range rooms {
		ok = <-acks && ok
	}
	return ok
}

// Wait blocks until every room has stopped (after ctx is cancelled). No room
// starts once it has begun.
func (l *Lobby[S, M, In, X]) Wait() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.running.Wait()
}

// freeCode needs l.mu held.
func (l *Lobby[S, M, In, X]) freeCode() (string, error) {
	for range codeTries {
		var b [codeLen]byte
		rand.Read(b[:]) // never fails (crypto/rand panics instead)
		for i := range b {
			b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
		}
		if _, taken := l.rooms[string(b[:])]; !taken {
			return string(b[:]), nil
		}
	}
	return "", ErrNoCode
}

// Get finds a room by code, case-insensitively; malformed codes never match.
func (l *Lobby[S, M, In, X]) Get(code string) (*room.Room[M, In, X], bool) {
	code, ok := NormalizeCode(code)
	if !ok {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.rooms[code]
	return r, r != nil
}

// live is every built room (reserved codes skipped).
func (l *Lobby[S, M, In, X]) live() []*room.Room[M, In, X] {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*room.Room[M, In, X], 0, len(l.rooms))
	for _, r := range l.rooms {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// List is the listed rooms' summaries, most humans first, then newest.
// Private rooms never appear.
func (l *Lobby[S, M, In, X]) List() []room.Summary[X] {
	var out []room.Summary[X]
	for _, r := range l.live() {
		if s := r.Summary(); s.Listed {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b room.Summary[X]) int {
		if c := cmp.Compare(b.Humans, a.Humans); c != 0 {
			return c
		}
		return cmp.Compare(b.Seq, a.Seq)
	})
	return out
}

// Quick picks the first listed room in List order with a free seat. The
// room may fill or close before the caller joins; the caller falls back.
func (l *Lobby[S, M, In, X]) Quick() (*room.Room[M, In, X], bool) {
	l.mu.Lock()
	draining := l.draining
	l.mu.Unlock()
	if draining {
		return nil, false
	}
	for _, s := range l.List() {
		if s.Humans < s.Seats {
			if r, ok := l.Get(s.Code); ok {
				return r, true
			}
		}
	}
	return nil, false
}

// NormalizeCode upper-cases code and checks it against ^[A-HJ-NP-Z2-9]{4}$.
func NormalizeCode(code string) (string, bool) {
	if len(code) != codeLen {
		return "", false
	}
	b := []byte(code)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
			b[i] = c
		}
		if !inAlphabet(c) {
			return "", false
		}
	}
	return string(b), true
}

func inAlphabet(c byte) bool {
	for i := range len(codeAlphabet) {
		if codeAlphabet[i] == c {
			return true
		}
	}
	return false
}

// build makes the game and its room for a reserved code. A factory error or
// panic is an error, and the reservation is released so the code (and a
// room slot) is not burned.
func (l *Lobby[S, M, In, X]) build(code string, s S, seq int64) (r *room.Room[M, In, X], err error) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("room build panic", "code", code, "panic", v)
			err = fmt.Errorf("lobby: building room: %v", v)
		}
		if err != nil {
			l.mu.Lock()
			delete(l.rooms, code)
			l.mu.Unlock()
			r = nil
		}
	}()
	g, err := l.newGame(s)
	if err != nil {
		slog.Warn("room build failed", "code", code, "err", err)
		return nil, fmt.Errorf("lobby: building room: %w", err)
	}
	o := l.ro
	o.Seq, o.Metrics = seq, l.m
	return room.New(code, g, o), nil
}
