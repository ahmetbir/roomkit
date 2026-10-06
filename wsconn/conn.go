// Package wsconn wraps a WebSocket with a bounded, non-blocking outbound
// queue and a reader goroutine, so a room actor never blocks on a socket.
package wsconn

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type Options struct {
	Lag          time.Duration // artificial outbound delay (tests / -lag)
	QueueSize    int           // outbound queue; default 64
	StallTimeout time.Duration // queue full this long -> close; default 2s
	ReadLimit    int64         // max inbound message bytes; default 2048
	IdleTimeout  time.Duration // no inbound message this long -> close; default 30s
	Origins      []string      // Accept origin patterns; empty = same host only
	Count        Counters      // nil = not counted
}

// Counters observe a connection's outbound side; called from its goroutines.
type Counters interface {
	Out()  // a message was written
	Drop() // a snapshot was lost to a full queue (evicted or refused)
}

const (
	inQueue      = 64
	writeTimeout = 5 * time.Second
)

func (o Options) withDefaults() Options {
	if o.QueueSize <= 0 {
		o.QueueSize = 64
	}
	if o.StallTimeout <= 0 {
		o.StallTimeout = 2 * time.Second
	}
	if o.ReadLimit <= 0 {
		o.ReadLimit = 2048
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = 30 * time.Second
	}
	return o
}

// Conn is safe for one sender goroutine plus any number of Close callers.
type Conn struct {
	ws        *websocket.Conn
	o         Options
	mu        sync.Mutex // guards q and fullSince
	q         *outQueue
	fullSince time.Time     // when the queue was first seen full; zero = not full
	wake      chan struct{} // cap 1: queue became non-empty
	in        chan []byte
	done      chan struct{}
	ctx       context.Context // cancelled by abort (and when the writer ends); bounds writes
	cancel    context.CancelFunc
	closeOnce sync.Once
	stalled   atomic.Bool
	closing   atomic.Pointer[closeInfo] // close status and reason the writer sends; nil = normal, no reason
	wg        sync.WaitGroup
}

// Accept upgrades the request. Origins are checked by the library (never
// skipped); compression is off since messages are small JSON.
func Accept(w http.ResponseWriter, r *http.Request, o Options) (*Conn, error) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  o.Origins,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	return Wrap(ws, o), nil
}

// Wrap starts the reader and writer goroutines on an established socket.
func Wrap(ws *websocket.Conn, o Options) *Conn {
	o = o.withDefaults()
	ws.SetReadLimit(o.ReadLimit)
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		ws: ws, o: o,
		q:    newOutQueue(o.QueueSize),
		wake: make(chan struct{}, 1),
		in:   make(chan []byte, inQueue),
		done: make(chan struct{}),
		ctx:  ctx, cancel: cancel,
	}
	c.wg.Go(c.readLoop)
	c.wg.Go(c.writeLoop)
	return c
}

// Send queues v for the writer without blocking. On a full queue the oldest
// queued Replaceable (snapshot) is evicted to make room; it returns false
// only if v was dropped because nothing could be evicted. A queue that
// stays full for StallTimeout closes the connection.
func (c *Conn) Send(v any) bool {
	return c.enqueue(outMsg{v: v, at: time.Now()})
}

// Fail queues v as the last message, then closes with StatusPolicyViolation.
func (c *Conn) Fail(v any) {
	c.closing.CompareAndSwap(nil, &closeInfo{code: websocket.StatusPolicyViolation})
	if !c.enqueue(outMsg{v: v, at: time.Now(), last: true}) {
		c.Close()
	}
}

func (c *Conn) enqueue(m outMsg) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	c.mu.Lock()
	stalled := false
	if !c.q.full() {
		c.fullSince = time.Time{}
	} else if c.fullSince.IsZero() {
		c.fullSince = time.Now()
	} else {
		stalled = time.Since(c.fullSince) > c.o.StallTimeout
	}
	full := c.q.full()
	ok := c.q.push(m)
	c.mu.Unlock()
	// Full and pushed: an older snapshot was evicted. Full and refused: m
	// itself is lost, a snapshot drop only if m is one.
	if full && (ok || replaceable(m)) && c.o.Count != nil {
		c.o.Count.Drop()
	}
	if stalled {
		c.abort()
		return false
	}
	if ok {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
	return ok
}

func (c *Conn) next() (outMsg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.q.pop()
}

func (c *Conn) Recv() <-chan []byte   { return c.in }
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close never blocks and is safe from any goroutine, an actor included: it
// signals both goroutines; the writer closes the socket after the write in
// flight, if any, so the close frame (code and reason) always follows a
// complete message.
func (c *Conn) Close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// abort tears down without a handshake: the write in flight is cancelled.
func (c *Conn) abort() {
	c.stalled.Store(true)
	c.cancel()
	c.Close()
}

// closeInfo is the close frame the writer sends: code and reason are set
// together (one atomic pointer), so a close never goes out without its reason.
type closeInfo struct {
	code   websocket.StatusCode
	reason string
}

// Restart closes with StatusServiceRestart (1012) and reason: the server is
// being replaced and the client should reconnect.
func (c *Conn) Restart(reason string) {
	c.closing.CompareAndSwap(nil, &closeInfo{code: websocket.StatusServiceRestart, reason: reason})
	c.Close()
}

func (c *Conn) closeWith(code websocket.StatusCode) {
	c.closing.CompareAndSwap(nil, &closeInfo{code: code})
	c.Close()
}

// Wait blocks until both goroutines have exited. After Close it can take up
// to the write timeout (a write in flight to a slow reader) plus the close
// handshake (bounded by the library's close timeout). Never call it from an
// actor goroutine; wait on Done or run it off to the side.
func (c *Conn) Wait() { c.wg.Wait() }

func (c *Conn) recoverPanic(where string) {
	if v := recover(); v != nil {
		slog.Error("wsconn panic", "where", where, "panic", v)
		c.abort()
		c.ws.CloseNow()
	}
}

// readLoop forwards text messages to in, dropping new ones while it is full
// (inputs repeat at 60 Hz). Any error, a binary message or IdleTimeout
// without a message ends the connection.
func (c *Conn) readLoop() {
	defer close(c.in)
	defer c.recoverPanic("reader")
	for {
		ctx, cancel := context.WithTimeout(context.Background(), c.o.IdleTimeout)
		typ, b, err := c.ws.Read(ctx)
		cancel()
		if err != nil {
			c.Close()
			return
		}
		if typ != websocket.MessageText {
			c.closeWith(websocket.StatusUnsupportedData)
			return
		}
		select {
		case c.in <- b:
		default:
		}
	}
}

// writeLoop marshals and writes queued messages, then performs the close:
// a handshake normally, an immediate close after a stall or write error.
func (c *Conn) writeLoop() {
	defer c.cancel()
	defer c.recoverPanic("writer")
	graceful := c.writeAll()
	c.Close()
	if !graceful || c.stalled.Load() {
		c.ws.CloseNow()
		return
	}
	ci := closeInfo{code: websocket.StatusNormalClosure}
	if p := c.closing.Load(); p != nil {
		ci = *p
	}
	c.ws.Close(ci.code, ci.reason)
}

// writeAll runs until Close or a final message; it reports false on a
// write or marshal error.
func (c *Conn) writeAll() bool {
	for {
		m, ok := c.next()
		if !ok {
			select {
			case <-c.done:
				return true
			case <-c.wake:
			}
			continue
		}
		if c.o.Lag > 0 {
			t := time.NewTimer(time.Until(m.at.Add(c.o.Lag)))
			select {
			case <-c.done:
				t.Stop()
				return true
			case <-t.C:
			}
		}
		select {
		case <-c.done:
			return true
		default:
		}
		b, err := json.Marshal(m.v)
		if err != nil {
			slog.Error("wsconn marshal", "err", err)
			return false
		}
		ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
		err = c.ws.Write(ctx, websocket.MessageText, b)
		cancel()
		if err != nil {
			return false
		}
		if c.o.Count != nil {
			c.o.Count.Out()
		}
		if m.last {
			return true
		}
	}
}
