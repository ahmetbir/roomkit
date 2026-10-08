package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"regexp"
	"slices"

	"github.com/ahmetbir/roomkit/limit"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/pilot"
	"github.com/ahmetbir/roomkit/room"
)

// handshake reads hello then create|join|quick and seats the player. On failure
// it returns the user-facing error text.
func (s *Server[S, M, In, X]) handshake(ctx context.Context, p *peer[M]) (*room.Seat[M], string) {
	m, err := s.recv(ctx, p)
	if err != nil {
		return nil, s.msgOf(err, p)
	}
	h := m.Head()
	if h.T != netproto.THello {
		return nil, msgBad
	}
	if h.V != s.kit.Version() {
		return nil, msgVersion
	}
	who := identify(netproto.CleanName(h.Name), h.Tok)
	who.Addr = p.addr

	m, err = s.recv(ctx, p)
	if err != nil {
		return nil, s.msgOf(err, p)
	}
	if s.draining() {
		return s.updating(p)
	}
	var rm *room.Room[M, In, X]
	switch h = m.Head(); h.T {
	case netproto.TCreate, netproto.TQuick, netproto.TJoin:
		if msg := s.vet(p, who); msg != "" {
			return nil, msg
		}
	}
	switch h.T {
	case netproto.TCreate:
		st, ok := s.kit.Settings(m, s.o.Now())
		if !ok {
			return nil, msgBadRoom
		}
		var msg string
		if rm, msg = s.create(p, st); msg != "" {
			return nil, msg
		}
	case netproto.TQuick:
		if seat, msg, done := s.quickJoin(ctx, p, who); done {
			return seat, msg
		}
		var msg string
		if rm, msg = s.create(p, s.kit.QuickSettings(s.o.Now())); msg != "" {
			return nil, msg
		}
	case netproto.TJoin:
		if s.joinFails.Blocked(p.key, s.o.Now()) {
			s.rejects.note(s.keyedReason(s.joinFails, p.key, "join-fail-rate"), p.ip)
			return nil, msgJoins
		}
		// Like creates: take the token first, give it back if no room is
		// found (that attempt is counted as a failed join instead).
		if !s.joins.Allow(p.key, s.o.Now()) {
			s.rejects.note(s.keyedReason(s.joins, p.key, "join-rate"), p.ip)
			return nil, msgJoins
		}
		var ok bool
		if rm, ok = s.lobby.Get(h.Code); !ok {
			s.joins.Refund(p.key, s.o.Now())
			s.joinFails.Allow(p.key, s.o.Now())
			return nil, msgNoRoom
		}
	default:
		return nil, msgBad
	}

	seat, err := rm.Join(ctx, who, roomConn{p.conn, s.draining})
	var rf *room.Refusal
	switch {
	case errors.Is(err, room.ErrFull):
		return nil, msgFull
	case errors.Is(err, room.ErrClosed):
		return nil, msgNoRoom
	case errors.As(err, &rf):
		return nil, p.refuse(rf.Code)
	case err != nil:
		return nil, s.msgOf(err, p)
	}
	return seat, ""
}

// vet asks the Kit's Admitter, if it has one, whether who may take a seat:
// "" admits, anything else is the refusal's error text. It runs before any
// room is picked, made or joined and before any create or join token is
// spent.
func (s *Server[S, M, In, X]) vet(p *peer[M], who room.Who) string {
	a, ok := s.kit.(Admitter)
	if !ok {
		return ""
	}
	if code, ok := a.Admit(who); !ok {
		return p.refuse(code)
	}
	return ""
}

// refuse is the error text of a game's refusal: its code, which the error
// frame also carries (fail). A code outside the protocol's shape is a game
// bug; the player then gets the generic full-room error.
func (p *peer[M]) refuse(code string) string {
	if !refusalCode.MatchString(code) || slices.Contains(netproto.ErrorCodes(), code) || slices.Contains(netproto.APICodes(), code) {
		slog.Error("game refusal with a bad code", "code", code)
		return msgFull
	}
	p.refused = code
	return code
}

var refusalCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// identify is the hello's player: a valid token is kept, anything else is
// replaced by a fresh one that the welcome hands back. Only the hash is kept.
func identify(name, tok string) room.Who {
	fresh := ""
	if !pilot.Valid(tok) {
		tok = pilot.New()
		fresh = tok
	}
	return room.Who{Name: name, Pilot: pilot.Hash(tok), NewToken: fresh}
}

// create makes a room under the address's create limit. On failure it
// returns the user-facing error text.
func (s *Server[S, M, In, X]) create(p *peer[M], st S) (*room.Room[M, In, X], string) {
	// Take the token first (check and spend in one step, so concurrent
	// creates cannot share one); a create that fails gives it back.
	if !s.creates.Allow(p.key, s.o.Now()) {
		s.rejects.note(s.keyedReason(s.creates, p.key, "create-rate"), p.ip)
		return nil, msgCreates
	}
	rm, err := s.lobby.Create(st)
	if err != nil {
		s.creates.Refund(p.key, s.o.Now()) // a full server costs no create token
	}
	switch {
	case errors.Is(err, lobby.ErrDraining):
		p.conn.Restart(msgUpdating) // the caller's fail is then a no-op
		return nil, msgUpdating
	case errors.Is(err, lobby.ErrBusy):
		s.rejects.note("max-rooms", p.ip)
		return nil, msgBusy
	case err != nil:
		slog.Error("create room", "err", err)
		return nil, msgNoCreate
	}
	return rm, ""
}

// quickJoin joins the room quick play picks, under the address's join
// limit. done is false when no listed room has a free seat, or the picked
// one filled or closed before the join (its token is given back): the
// caller then makes a new room.
func (s *Server[S, M, In, X]) quickJoin(ctx context.Context, p *peer[M], who room.Who) (seat *room.Seat[M], msg string, done bool) {
	r, ok := s.quickPick()
	if !ok {
		return nil, "", false
	}
	if !s.joins.Allow(p.key, s.o.Now()) {
		s.rejects.note(s.keyedReason(s.joins, p.key, "join-rate"), p.ip)
		return nil, msgJoins, true
	}
	seat, err := r.Join(ctx, who, roomConn{p.conn, s.draining})
	var rf *room.Refusal
	switch {
	case err == nil:
		return seat, "", true
	case errors.Is(err, room.ErrFull), errors.Is(err, room.ErrClosed), errors.As(err, &rf):
		s.joins.Refund(p.key, s.o.Now())
		return nil, "", false
	}
	return nil, s.msgOf(err, p), true
}

// keyedReason names a Keyed refusal: its own reason, or "limiter-full" when
// the table refused a new address (key) because it is at capacity.
func (s *Server[S, M, In, X]) keyedReason(k *limit.Keyed, key, reason string) string {
	if k.Full() && !k.Known(key) {
		return "limiter-full"
	}
	return reason
}

// recv waits for the next handshake message, answering pings on the way.
func (s *Server[S, M, In, X]) recv(ctx context.Context, p *peer[M]) (M, error) {
	var zero M
	for {
		select {
		case <-ctx.Done():
			return zero, errTimeout
		case b, ok := <-p.conn.Recv():
			if !ok {
				return zero, net.ErrClosed
			}
			m, err := s.next(p, b)
			if err != nil {
				return m, err
			}
			if h := m.Head(); h.T == netproto.TPing {
				p.conn.Send(netproto.NewPong(h.TS))
				continue
			}
			return m, nil
		}
	}
}
