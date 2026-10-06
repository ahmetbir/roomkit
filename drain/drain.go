// Package drain is the blue/green handover actor every game shares.
//
// The new version starts while the old one still serves; the proxy is
// switched to the new one; then the old one gets SIGUSR1 (drain). A draining
// server keeps its players, refuses everyone else (WebSocket close 1012,
// /api 503), hands its single-writer store over at once (rooms flush their
// open tallies, the store takes its final snapshot and releases its lock, so
// the new server's store opens) and exits when its last game socket closes
// or after Options.Max. SIGUSR2 (rollback to this server) undoes a drain: it
// serves again and acquires the store again. Drain/undrain is the only
// store handoff.
package drain

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// Server is the server side of a drain (core/server's Server).
type Server interface {
	Drain(on bool)
	Conns() int
	FlushStats(ctx context.Context) bool // rooms hand over their open tallies
}

// Handoff is the game's single-writer store that moves between servers.
type Handoff interface {
	// Acquire opens the store; it returns once the store is open, off for
	// good, or ctx ends (it may wait for another server's lock meanwhile).
	Acquire(ctx context.Context)
	// Release takes the final snapshot and releases the store's lock.
	Release() error
	// Reopen lets a released store be acquired again (undrain).
	Reopen()
}

// Options time the actor; zero values take the defaults.
type Options struct {
	Every     time.Duration    // how often a draining server counts its sockets; default 1 s
	Max       time.Duration    // a draining server quits after this at the latest; default 30 min
	FlushWait time.Duration    // a stuck room must not hold the handoff; default 2 s
	Now       func() time.Time // default time.Now
}

const (
	defaultEvery     = time.Second
	defaultMax       = 30 * time.Minute
	defaultFlushWait = 2 * time.Second
)

func (o Options) withDefaults() Options {
	if o.Every <= 0 {
		o.Every = defaultEvery
	}
	if o.Max <= 0 {
		o.Max = defaultMax
	}
	if o.FlushWait <= 0 {
		o.FlushWait = defaultFlushWait
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Actor owns the drain state of one server.
type Actor struct {
	srv  Server
	ho   Handoff // nil = no store
	quit func()  // starts the normal shutdown
	o    Options
}

// New builds the actor for srv; ho may be nil (no store to hand over).
func New(srv Server, ho Handoff, quit func(), o Options) Actor {
	return Actor{srv: srv, ho: ho, quit: quit, o: o.withDefaults()}
}

// Run is the drain actor: it owns the drain state until ctx ends or it quits.
func (a Actor) Run(ctx context.Context, sig <-chan os.Signal) {
	stopAcquire := a.startAcquire(ctx)
	defer func() { stopAcquire() }()
	t := time.NewTicker(a.o.Every)
	defer t.Stop()
	var since time.Time // zero = not draining
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-sig:
			switch {
			case s == syscall.SIGUSR1 && since.IsZero():
				since = a.o.Now()
				a.srv.Drain(true)
				stopAcquire()
				a.flushRooms(ctx)
				a.release()
				slog.Info("draining", "conns", a.srv.Conns(), "max", a.o.Max.String())
			case s == syscall.SIGUSR2 && !since.IsZero():
				since = time.Time{}
				a.srv.Drain(false)
				if a.ho != nil {
					a.ho.Reopen()
				}
				stopAcquire = a.startAcquire(ctx)
				slog.Info("drain undone: serving")
			}
		case <-t.C:
			if since.IsZero() {
				continue
			}
			n := a.srv.Conns()
			if n == 0 || a.o.Now().Sub(since) >= a.o.Max {
				slog.Info("drained: stopping", "conns", n, "after", a.o.Now().Sub(since).Round(time.Second).String())
				a.quit()
				return
			}
		}
	}
}

// startAcquire opens the store in the background; the returned func stops
// a wait that is still going (and is safe to call twice).
func (a Actor) startAcquire(ctx context.Context) func() {
	if a.ho == nil {
		return func() {}
	}
	actx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); a.ho.Acquire(actx) }()
	return func() { cancel(); <-done }
}

// flushRooms has every room record its players' open tallies (as a leave)
// while the store is still open: the store must queue them ahead of its
// final snapshot (Release).
func (a Actor) flushRooms(ctx context.Context) {
	if a.ho == nil {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, a.o.FlushWait)
	defer cancel()
	if !a.srv.FlushStats(fctx) {
		slog.Error("stats flush", "err", "a room did not flush in time; its open tallies are lost")
	}
}

func (a Actor) release() {
	if a.ho == nil {
		return
	}
	if err := a.ho.Release(); err != nil {
		slog.Error("stats close", "err", err)
	}
	slog.Info("stats handed off")
}
