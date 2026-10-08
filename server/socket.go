package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/ahmetbir/roomkit/limit"
	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/room"
	"github.com/ahmetbir/roomkit/wsconn"
)

// User-facing error texts.
const (
	msgVersion  = "sürüm uyuşmuyor, sayfayı yenile"
	msgNoRoom   = "oda bulunamadı"
	msgBadRoom  = "geçersiz oda ayarı"
	msgFull     = "oda dolu"
	msgBad      = "geçersiz mesaj"
	msgNoCreate = "oda kurulamadı"
	msgBusy     = "sunucu dolu"
	msgCreates  = "çok fazla oda kurdun, biraz bekle"
	msgJoins    = "çok fazla deneme, biraz bekle"
	msgFlood    = "çok fazla mesaj"
	msgConns    = "çok fazla bağlantı"
	msgTimeout  = "zaman aşımı"
)

var (
	errTimeout = errors.New("server: handshake timeout")
	errFlood   = errors.New("server: message rate exceeded")
	errDropped = errors.New("server: input over its rate, dropped")
	errDecode  = errors.New("server: undecodable message")
)

// floodError is errFlood naming the bucket that refused and the message type.
type floodError struct{ bucket, typ string }

func (e floodError) Error() string        { return errFlood.Error() + ": " + e.bucket + " bucket, " + e.typ }
func (e floodError) Is(target error) bool { return target == errFlood }

// peer is one game socket: its connection, its address and its limits.
type peer[M any] struct {
	conn    *wsconn.Conn
	ip      string     // for logs
	addr    netip.Addr // the game's room.Who.Addr
	key     string     // for per-address limits
	guard   *msgGuard
	held    M       // one-shot presses of dropped inputs
	drops   dropLog // inputs dropped over their rate
	refused string  // the game's refusal code, when the handshake failed with one
}

func (s *Server[S, M, In, X]) socket(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.o.TrustProxy)
	p := &peer[M]{ip: ip.String(), addr: ip, key: limitKey(ip), guard: newMsgGuard(s.o.Limits, s.o.Now)}
	if err := s.conns.Acquire(p.key); err != nil {
		if isNet(err) {
			s.rejects.note("conns-per-net", p.ip)
			http.Error(w, msgConns, http.StatusTooManyRequests)
		} else if errors.Is(err, limit.ErrKey) {
			s.rejects.note("conns-per-ip", p.ip)
			http.Error(w, msgConns, http.StatusTooManyRequests)
		} else {
			s.rejects.note("conns-total", p.ip)
			http.Error(w, msgBusy, http.StatusServiceUnavailable)
		}
		return
	}
	s.connsGauge(1)
	defer func() { s.conns.Release(p.key); s.connsGauge(-1) }()
	s.sockets.Add(1) // before the upgrade: Shutdown still tracks this request
	defer s.sockets.Done()

	conn, err := wsconn.Accept(w, r, wsconn.Options{Lag: s.o.Lag, Origins: s.o.Origins, Count: s.counters()})
	if err != nil {
		return // Accept has written the HTTP error
	}
	defer conn.Wait()
	p.conn = conn
	ctx, cancel := context.WithTimeout(r.Context(), s.o.HandshakeTimeout)
	seat, msg := s.handshake(ctx, p)
	cancel()
	if msg != "" {
		fail(conn, msg, p.refused)
		return
	}
	defer seat.Leave()
	if msg := s.pump(p, seat); msg != "" {
		fail(conn, msg, "")
		return
	}
	conn.Close()
}

// fail sends a final error message (with its code) and closes with
// StatusPolicyViolation. refused is a game's refusal code: msg is then that
// code, and so is the frame's code.
func fail(conn *wsconn.Conn, msg, refused string) {
	code := errCode(msg)
	if refused != "" && msg == refused {
		code = refused
	}
	conn.Fail(netproto.NewError(code, msg))
	<-conn.Done()
}

// next decodes one inbound message within the connection's rate limits:
// errDropped for an input over its rate, a floodError for anything else,
// errDecode for a frame the game cannot decode. The hard ceiling counts the
// raw frame before the game's decoder sees it.
func (s *Server[S, M, In, X]) next(p *peer[M], b []byte) (M, error) {
	var zero M
	if s.o.Metrics != nil {
		s.o.Metrics.MsgsIn.Inc()
	}
	if v, lim := p.guard.frame(len(b)); v == kick {
		return zero, floodError{lim, "any"}
	}
	m, err := s.kit.Decode(b)
	if err != nil {
		// %v, not %w: whatever the game's error wraps, it is a bad message.
		return zero, fmt.Errorf("%w: %v", errDecode, err)
	}
	t := m.Head().T
	switch v, bucket := p.guard.check(t, s.class(t)); v {
	case drop:
		return m, errDropped
	case kick:
		return m, floodError{bucket, t}
	}
	return m, nil
}

// class is the rate class of message type t: the game's, except for the
// core types, which the guard rates by their own rules.
func (s *Server[S, M, In, X]) class(t string) Class {
	if coreType(t) {
		return ClassAll
	}
	return s.kit.Class(t)
}

// admit is next for the game: an input over its rate is dropped (ok false)
// and its one-shot presses carry over to the next admitted input.
func (s *Server[S, M, In, X]) admit(p *peer[M], b []byte) (m M, ok bool, err error) {
	m, err = s.next(p, b)
	switch {
	case errors.Is(err, errDropped):
		p.held = p.held.Latch(m)
		p.drops.note(p.guard.now(), p.ip)
		return m, false, nil
	case err != nil:
		return m, false, err
	}
	if m.Head().T == netproto.TIn {
		m = m.Latch(p.held)
		var zero M
		p.held = zero
	}
	return m, true, nil
}

func (s *Server[S, M, In, X]) msgOf(err error, p *peer[M]) string {
	switch {
	case errors.Is(err, errFlood):
		var fe floodError
		errors.As(err, &fe)
		s.rejects.note("flood", p.ip, "bucket", fe.bucket, "type", fe.typ)
		return msgFlood
	case errors.Is(err, errTimeout), errors.Is(err, context.DeadlineExceeded):
		return msgTimeout
	}
	return msgBad
}

// pump forwards in-game messages to the room until the connection or the
// room ends. It returns an error text for a message that breaks protocol
// or the connection's rate limits. The core types are decided here: in,
// ping and chat go to the room, the handshake's are refused; only a game
// type is the Kit's to admit.
func (s *Server[S, M, In, X]) pump(p *peer[M], seat *room.Seat[M]) string {
	defer p.drops.flush(p.ip)
	done := seat.Done()
	for {
		select {
		case <-done:
			return ""
		case b, ok := <-p.conn.Recv():
			if !ok {
				return ""
			}
			m, ok, err := s.admit(p, b)
			if err != nil {
				return s.msgOf(err, p)
			}
			if !ok {
				continue
			}
			switch t := m.Head().T; {
			case t == netproto.TIn, t == netproto.TPing, t == netproto.TChat:
			case coreType(t): // the handshake is over
				return msgBad
			case !s.kit.InRoom(m):
				return msgBad
			}
			seat.Input(m)
		}
	}
}
