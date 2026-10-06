package wsconn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// serve starts a server whose handler runs fn on every accepted Conn and
// returns a dialed client.
func serve(t *testing.T, o Options, fn func(*Conn)) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, o)
		if err != nil {
			return
		}
		fn(c)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	return ws
}

func echo(c *Conn) {
	for b := range c.Recv() {
		c.Send(json.RawMessage(b))
	}
}

func roundTrip(t *testing.T, ws *websocket.Conn, msg string) (string, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := ws.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	_, b, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), time.Since(start)
}

func TestEcho(t *testing.T) {
	ws := serve(t, Options{}, echo)
	if got, _ := roundTrip(t, ws, `{"t":"ping","ts":1.5}`); got != `{"t":"ping","ts":1.5}` {
		t.Fatalf("got %s", got)
	}
}

func TestLag(t *testing.T) {
	ws := serve(t, Options{Lag: 80 * time.Millisecond}, echo)
	if _, d := roundTrip(t, ws, `{"t":"ping"}`); d < 80*time.Millisecond {
		t.Fatalf("round trip %v < 80ms", d)
	}
}

func TestStallCloses(t *testing.T) {
	done := make(chan *Conn, 1)
	serve(t, Options{QueueSize: 2, StallTimeout: 100 * time.Millisecond}, func(c *Conn) {
		big := strings.Repeat("x", 64<<10)
		deadline := time.After(time.Second)
		for {
			select {
			case <-c.Done():
				done <- c
				return
			case <-deadline:
				done <- nil
				return
			default:
			}
			c.Send(big)
			time.Sleep(time.Millisecond)
		}
	})
	if c := <-done; c == nil {
		t.Fatal("connection not closed within 1s while client never reads")
	}
}

func TestCloseEndsRecv(t *testing.T) {
	got := make(chan bool, 1)
	ws := serve(t, Options{}, func(c *Conn) {
		c.Close()
		c.Close() // idempotent
		select {
		case _, ok := <-c.Recv():
			for ok {
				_, ok = <-c.Recv()
			}
			c.Wait()
			got <- true
		case <-time.After(2 * time.Second):
			got <- false
		}
	})
	go ws.Read(t.Context()) // a live peer answers the close handshake
	if !<-got {
		t.Fatal("Recv not closed after Close")
	}
}

func TestBinaryAndOversizeClose(t *testing.T) {
	for name, tc := range map[string]struct {
		typ  websocket.MessageType
		size int
		want websocket.StatusCode
	}{
		"binary":   {websocket.MessageBinary, 10, websocket.StatusUnsupportedData},
		"oversize": {websocket.MessageText, 4096, websocket.StatusMessageTooBig},
	} {
		t.Run(name, func(t *testing.T) {
			ws := serve(t, Options{}, func(c *Conn) {
				for range c.Recv() {
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := ws.Write(ctx, tc.typ, make([]byte, tc.size)); err != nil {
				t.Fatal(err)
			}
			_, _, err := ws.Read(ctx)
			if got := websocket.CloseStatus(err); got != tc.want {
				t.Fatalf("close status %v (%v), want %v", got, err, tc.want)
			}
		})
	}
}

func TestIdleTimeout(t *testing.T) {
	ws := serve(t, Options{IdleTimeout: 100 * time.Millisecond}, func(c *Conn) {
		for range c.Recv() {
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, _, err := ws.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("idle connection not closed by server: %v", err)
	}
}

func TestFailDeliversThenCloses(t *testing.T) {
	ws := serve(t, Options{}, func(c *Conn) {
		c.Fail(map[string]string{"t": "error", "msg": "oda bulunamadı"})
		<-c.Done()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, b, err := ws.Read(ctx)
	if err != nil || !strings.Contains(string(b), "oda bulunamadı") {
		t.Fatalf("got %s, %v", b, err)
	}
	if _, _, err := ws.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("want policy-violation close, got %v", err)
	}
}

// Restart closes with 1012 and the reason (blue/green drain: reconnect).
func TestRestartCloses1012WithReason(t *testing.T) {
	ws := serve(t, Options{}, func(c *Conn) { c.Restart("sunucu güncelleniyor"); c.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, _, err := ws.Read(ctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.StatusServiceRestart || ce.Reason != "sunucu güncelleniyor" {
		t.Fatalf("close: %v", err)
	}
}
