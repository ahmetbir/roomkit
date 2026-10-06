package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/netproto"
)

const (
	dialTimeout = 10 * time.Second // dial + hello + create/join + welcome
	readLimit   = 8 << 20          // a welcome may carry large static data
)

// roomCode hands the creator's room code to the room's joiners.
type roomCode struct {
	once  sync.Once
	ready chan struct{}
	code  string
}

func newRoomCode() *roomCode { return &roomCode{ready: make(chan struct{})} }

func (c *roomCode) set(code string) {
	c.once.Do(func() { c.code = code; close(c.ready) })
}

func (c *roomCode) wait(ctx context.Context) (string, bool) {
	select {
	case <-c.ready:
		return c.code, true
	case <-ctx.Done():
		return "", false
	}
}

// inbound is the part of any server message the load test reads; the rest
// of a snapshot is scanned but not kept, and the rest of any other message
// is the Script's.
type inbound struct {
	T    string  `json:"t"`
	Tick int     `json:"tick"`
	Code string  `json:"code"`
	You  int     `json:"you"`
	Msg  string  `json:"msg"`
	TS   float64 `json:"ts"`
}

// player is one simulated client.
type player struct {
	i     int
	slot  Slot
	url   string
	sc    Script
	pace  pace
	code  *roomCode // nil for quick play
	st    *Stats
	epoch time.Time    // ping timestamps are ms since it
	buf   bytes.Buffer // reused read buffer
}

// run plays until ctx ends or the socket does; a socket that ends before
// ctx is recorded as a disconnect with its reason.
func (p *player) run(ctx context.Context) {
	start := time.Now()
	hctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	var entry any
	switch {
	case p.slot.Room < 0:
		entry = struct {
			T string `json:"t"`
		}{netproto.TQuick}
	case p.slot.Creator:
		entry = p.sc.Create()
	default:
		code, ok := p.code.wait(hctx)
		if !ok {
			p.end(ctx, errors.New("no room code"), "")
			return
		}
		entry = struct {
			T    string `json:"t"`
			Code string `json:"code"`
		}{netproto.TJoin, code}
	}
	conn, resp, err := websocket.Dial(hctx, p.url, nil)
	if err != nil {
		if resp != nil {
			p.st.Disconnect(fmt.Sprintf("dial:%d", resp.StatusCode))
			return
		}
		p.end(ctx, err, "")
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)
	if err := p.write(hctx, conn, p.sc.Hello(p.i)); err != nil {
		p.end(ctx, err, "")
		return
	}
	if err := p.write(hctx, conn, entry); err != nil {
		p.end(ctx, err, "")
		return
	}
	w, err := p.welcome(hctx, conn)
	if err != nil || w.T == "error" {
		p.end(ctx, err, w.Msg)
		return
	}
	p.st.Handshake.Observe(time.Since(start))
	if p.slot.Creator {
		p.code.set(w.Code)
	}
	p.st.Conns.Add(1)
	defer p.st.Conns.Add(-1)

	wctx, stop := context.WithCancel(ctx)
	replies := make(chan any, 1)
	done := make(chan struct{})
	go func() { defer close(done); p.send(wctx, conn, replies) }()
	msg, err := p.read(ctx, conn, w.You, replies)
	stop()
	<-done
	if ctx.Err() != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	p.end(ctx, err, msg)
}

// end records why the socket ended, unless the run itself is over.
func (p *player) end(ctx context.Context, err error, msg string) {
	if ctx.Err() != nil && msg == "" {
		return
	}
	p.st.Disconnect(Reason(err, msg))
}

// welcome reads until the welcome (or an error message).
func (p *player) welcome(ctx context.Context, conn *websocket.Conn) (inbound, error) {
	for {
		m, _, err := p.recv(ctx, conn)
		if err != nil || m.T == "welcome" || m.T == "error" {
			return m, err
		}
	}
}

// recv reads one message; raw is valid until the next recv.
func (p *player) recv(ctx context.Context, conn *websocket.Conn) (m inbound, raw []byte, err error) {
	_, r, err := conn.Reader(ctx)
	if err != nil {
		return inbound{}, nil, err
	}
	p.buf.Reset()
	if _, err := p.buf.ReadFrom(r); err != nil {
		return inbound{}, nil, err
	}
	b := p.buf.Bytes()
	p.st.MsgsIn.Add(1)
	p.st.BytesIn.Add(uint64(len(b)))
	if tick, ok := SnapTick(b); ok {
		return inbound{T: "snap", Tick: tick}, b, nil // most traffic: skip the full decode
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return inbound{}, nil, fmt.Errorf("bad json: %w", err)
	}
	return m, b, nil
}

// read consumes game messages: snapshot arrival intervals and tick gaps,
// pong round trips; any other message goes to the Script, whose reply (one
// in flight at most) the sender writes. It returns the server's error text, if any, and the read error.
func (p *player) read(ctx context.Context, conn *websocket.Conn, you int, replies chan<- any) (string, error) {
	var last time.Time
	lastTick := 0
	for {
		m, raw, err := p.recv(ctx, conn)
		if err != nil {
			return "", err
		}
		now := time.Now()
		switch m.T {
		case "snap":
			p.st.Snaps.Add(1)
			if !last.IsZero() {
				p.st.SnapIv.Observe(now.Sub(last))
			}
			if lastTick > 0 {
				p.st.Gaps.Add(gaps(m.Tick-lastTick, p.pace.snapEvery))
			}
			last, lastTick = now, m.Tick
		case "pong":
			sent := p.epoch.Add(time.Duration(m.TS * float64(time.Millisecond)))
			p.st.RTT.Observe(now.Sub(sent))
		case "error":
			return m.Msg, nil
		default:
			if reply := p.sc.React(p.i, you, m.T, raw); reply != nil {
				select {
				case replies <- reply:
				default: // one reply in flight at most; the game repeats if it must
				}
			}
		}
	}
}

// gaps is the number of snapshots lost between two that arrived d ticks
// apart, when the server snapshots every snapEvery ticks.
func gaps(d, snapEvery int) uint64 {
	if d <= snapEvery {
		return 0
	}
	return uint64(d/snapEvery - 1)
}

// send writes inputs at the input rate, a ping every second and the
// Script's reply once the reader has it, until ctx ends or a write fails.
func (p *player) send(ctx context.Context, conn *websocket.Conn, replies <-chan any) {
	t := time.NewTicker(time.Second / time.Duration(p.pace.inputHz))
	defer t.Stop()
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-replies:
			if p.write(ctx, conn, r) != nil {
				return
			}
		case <-t.C:
			seq++
			if p.write(ctx, conn, p.sc.Input(p.i, seq)) != nil {
				return
			}
			if seq%uint32(p.pace.inputHz) == 0 { // 1 s
				ts := float64(time.Since(p.epoch).Microseconds()) / 1000
				if p.write(ctx, conn, struct {
					T  string  `json:"t"`
					TS float64 `json:"ts"`
				}{netproto.TPing, ts}) != nil {
					return
				}
			}
		}
	}
}

func (p *player) write(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	p.st.MsgsOut.Add(1)
	p.st.BytesOut.Add(uint64(len(b)))
	return nil
}
