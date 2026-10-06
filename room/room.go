// Package room runs one match as an actor: a single goroutine owns the
// game and talks to connections only through channels and Sender.
package room

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/netproto"
)

// Option defaults.
const (
	DefaultTickRate      = 60
	DefaultEmptyTimeout  = 60 * time.Second
	DefaultUnusedTimeout = 15 * time.Second // a room no human has ever joined (S11)
	DefaultPublishEvery  = 60               // ticks between periodic summary publishes
	DefaultChatMax       = 6
	// seatInbox bounds one seat's messages in flight to the room; with at
	// most a dozen seats the shared queue never fills, so one flooding
	// client cannot crowd out the others' inputs.
	seatInbox  = 16
	inputQueue = 256
)

var ErrClosed = errors.New("room: closed")

// Sender is the outbound side of a connection. Send must not block.
type Sender interface {
	Send(v any) bool
	Close()
}

// Options are a room's ties to its lobby and its clock; zero fields take
// the defaults.
type Options struct {
	Seq           int64             // lobby creation order (newer rooms list first on equal humans)
	Metrics       *metrics.Registry // nil = not measured
	TickRate      int
	EmptyTimeout  time.Duration // no humans this long: the room ends
	UnusedTimeout time.Duration // no human ever joined this long: the room ends
	PublishEvery  int           // ticks between periodic summary publishes
	ChatMax       int           // highest quick-chat preset id
	ChatCooldown  int           // ticks between two relayed chats of one player; default 2 s
}

func (o Options) withDefaults() Options {
	if o.TickRate <= 0 {
		o.TickRate = DefaultTickRate
	}
	if o.EmptyTimeout <= 0 {
		o.EmptyTimeout = DefaultEmptyTimeout
	}
	if o.UnusedTimeout <= 0 {
		o.UnusedTimeout = DefaultUnusedTimeout
	}
	if o.PublishEvery <= 0 {
		o.PublishEvery = DefaultPublishEvery
	}
	if o.ChatMax <= 0 {
		o.ChatMax = DefaultChatMax
	}
	if o.ChatCooldown <= 0 {
		o.ChatCooldown = 2 * o.TickRate
	}
	return o
}

type inputMsg[M any] struct {
	seat *Seat[M]
	msg  M
}

type Room[M Msg[In], In Input[In], X any] struct {
	code    string
	label   string // game.Label at New: readable from any goroutine
	game    Game[M, In, X]
	o       Options
	summary atomic.Pointer[Summary[X]]
	joins   chan joinReq
	leaves  chan PlayerID
	inputs  chan inputMsg[M]
	flushes chan chan struct{} // FlushStats requests; closed reply = done
	done    chan struct{}

	// owned by the Run goroutine
	sessions   map[PlayerID]*session[In]
	ticks      int
	emptySince time.Time
	joined     bool // a human has been seated at least once
}

func New[M Msg[In], In Input[In], X any](code string, g Game[M, In, X], o Options) *Room[M, In, X] {
	r := &Room[M, In, X]{
		code: code, label: g.Label(), game: g, o: o.withDefaults(),
		joins:    make(chan joinReq),
		leaves:   make(chan PlayerID),
		inputs:   make(chan inputMsg[M], inputQueue),
		flushes:  make(chan chan struct{}),
		done:     make(chan struct{}),
		sessions: map[PlayerID]*session[In]{},
	}
	r.publish()
	r.gauge(0, int64(r.Summary().Seats)) // every seat starts as a bot
	return r
}

func (r *Room[M, In, X]) Code() string          { return r.code }
func (r *Room[M, In, X]) Label() string         { return r.label }
func (r *Room[M, In, X]) Done() <-chan struct{} { return r.done }

// Run is the actor loop. It returns on ctx cancel, after EmptyTimeout with
// no humans (UnusedTimeout if none ever joined), or on a panic; every
// session is closed on the way out.
func (r *Room[M, In, X]) Run(ctx context.Context) {
	defer close(r.done)
	defer r.closeAll()
	defer func() {
		if v := recover(); v != nil {
			slog.Error("room panic", "code", r.code, "panic", v)
		}
	}()
	t := time.NewTicker(time.Second / time.Duration(r.o.TickRate))
	defer t.Stop()
	r.emptySince = time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-r.joins:
			r.join(req)
		case id := <-r.leaves:
			r.leave(id)
		case in := <-r.inputs:
			<-in.seat.slots
			r.input(in.seat.id, in.msg)
		case ack := <-r.flushes:
			r.game.FlushStats()
			close(ack)
		case now := <-t.C:
			if len(r.sessions) == 0 && now.Sub(r.emptySince) >= r.emptyTimeout() {
				return
			}
			r.tick()
		}
	}
}

func (r *Room[M, In, X]) emptyTimeout() time.Duration {
	if r.joined {
		return r.o.EmptyTimeout
	}
	return r.o.UnusedTimeout
}

// closeAll hands the game's final tallies over (Game.Close), closes every
// session and returns the gauges. The game may have panicked already: a
// second panic in Close or Info is logged, and the gauges then come from
// the last published summary.
func (r *Room[M, In, X]) closeAll() {
	r.safely("close", r.game.Close)
	for _, s := range r.sessions {
		s.out.Close()
	}
	in := r.Summary().Info
	r.safely("info", func() { in = r.game.Info() })
	r.gauge(-int64(in.Humans), -int64(in.Seats-in.Humans))
}

func (r *Room[M, In, X]) safely(what string, f func()) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("room panic", "code", r.code, "in", what, "panic", v)
		}
	}()
	f()
}

// gauge moves the server-wide human and bot gauges.
func (r *Room[M, In, X]) gauge(humans, bots int64) {
	if r.o.Metrics != nil {
		r.o.Metrics.Humans.Add(humans)
		r.o.Metrics.Bots.Add(bots)
	}
}

func (r *Room[M, In, X]) input(id PlayerID, m M) {
	s, ok := r.sessions[id]
	if !ok {
		return
	}
	switch h := m.Head(); h.T {
	case netproto.TIn:
		s.q.push(h.Seq, m.Input())
	case netproto.TPing:
		s.out.Send(netproto.NewPong(h.TS))
	case netproto.TChat:
		r.chat(id, h.Chat)
	default:
		r.game.Handle(id, m, outbox[M, In, X]{r})
	}
}

func (r *Room[M, In, X]) tick() {
	if r.o.Metrics != nil {
		start := time.Now()
		defer func() {
			d := time.Since(start)
			r.o.Metrics.TickSeconds.Observe(d.Seconds())
			if d > time.Second/time.Duration(r.o.TickRate) {
				r.o.Metrics.TickOverruns.Inc()
			}
		}()
	}
	inputs := make(map[PlayerID]In, len(r.sessions))
	for id, s := range r.sessions {
		in, _ := s.q.next()
		if s.q.started() { // before its first input the seat keeps its own controls
			inputs[id] = in
		}
	}
	r.game.Step(inputs, outbox[M, In, X]{r})
	r.ticks++
	if r.ticks%r.o.PublishEvery == 0 {
		r.publish()
	}
}
