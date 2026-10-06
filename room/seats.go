package room

import (
	"context"
	"time"
)

type joinReq struct {
	who   Who
	out   Sender
	reply chan joinResp
}

type joinResp struct {
	id  PlayerID
	err error
}

// Join seats a human. It fails with ErrClosed if the room has stopped, an
// error wrapping ErrFull if no seat is left, or ctx's error.
func (r *Room[M, In, X]) Join(ctx context.Context, who Who, out Sender) (*Seat[M], error) {
	req := joinReq{who: who, out: out, reply: make(chan joinResp, 1)}
	select {
	case r.joins <- req:
	case <-r.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// The actor answers right away; waiting on ctx here could orphan a seat.
	var resp joinResp
	select {
	case resp = <-req.reply:
	case <-r.done:
		return nil, ErrClosed
	}
	if resp.err != nil {
		return nil, resp.err
	}
	return &Seat[M]{id: resp.id, slots: make(chan struct{}, seatInbox), inputs: r.inputs, leaves: r.leaves, done: r.done}, nil
}

// Seat is one joined human's handle on the room, used by its connection
// goroutine only.
type Seat[M any] struct {
	id     PlayerID
	slots  chan struct{} // messages in flight; the room frees one per message
	inputs chan<- inputMsg[M]
	leaves chan<- PlayerID
	done   <-chan struct{}
}

func (s *Seat[M]) ID() PlayerID { return s.id }

// Done is closed when the room stops.
func (s *Seat[M]) Done() <-chan struct{} { return s.done }

// Input hands a client message to the room. It drops the message if this
// seat already has seatInbox messages waiting, or the room is busy.
func (s *Seat[M]) Input(m M) {
	select {
	case s.slots <- struct{}{}:
	default:
		return
	}
	select {
	case s.inputs <- inputMsg[M]{s, m}:
	case <-s.done:
	default:
		<-s.slots
	}
}

func (s *Seat[M]) Leave() {
	select {
	case s.leaves <- s.id:
	case <-s.done:
	}
}

func (r *Room[M, In, X]) join(req joinReq) {
	id, err := r.game.Join(req.who)
	if err != nil {
		req.reply <- joinResp{err: err}
		return
	}
	r.sessions[id] = newSession[In](req.out)
	r.joined = true
	r.gauge(+1, -1) // the human took a bot's seat
	r.publish()     // before the reply: a lobby read after Join sees the seat
	req.reply <- joinResp{id: id}
	r.game.Welcome(id, r.code, req.who.NewToken, outbox[M, In, X]{r})
}

func (r *Room[M, In, X]) leave(id PlayerID) {
	if _, ok := r.sessions[id]; !ok {
		return
	}
	delete(r.sessions, id)
	r.game.Leave(id)
	r.gauge(-1, +1) // a bot takes the seat back
	if len(r.sessions) == 0 {
		r.emptySince = time.Now()
	}
	r.publish()
}
