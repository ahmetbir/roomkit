package wsconn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestCloseWaitsForWriteInFlight: a close with a code (1012 restart, 1003
// binary from the peer) requested while a message is still being written
// lets that write finish, then sends the close frame with code and reason.
func TestCloseWaitsForWriteInFlight(t *testing.T) {
	// larger than the loopback socket buffers: its write stays in flight
	// until the peer reads
	big := strings.Repeat("x", 16<<20)
	for name, tc := range map[string]struct {
		server func(*Conn)                 // after the big Send
		peer   func(*websocket.Conn) error // before the peer reads
		code   websocket.StatusCode
		reason string
	}{
		"restart": {
			server: func(c *Conn) { c.Restart("sunucu güncelleniyor") },
			peer:   func(*websocket.Conn) error { return nil },
			code:   websocket.StatusServiceRestart, reason: "sunucu güncelleniyor",
		},
		"binary": {
			server: func(*Conn) {},
			peer: func(ws *websocket.Conn) error {
				return ws.Write(t.Context(), websocket.MessageBinary, []byte{1})
			},
			code: websocket.StatusUnsupportedData,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ws := serve(t, Options{}, func(c *Conn) {
				c.Send(big)
				time.Sleep(100 * time.Millisecond) // the writer is blocked in Write
				tc.server(c)
				c.Wait()
			})
			ws.SetReadLimit(32 << 20)
			time.Sleep(50 * time.Millisecond)
			if err := tc.peer(ws); err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond) // the close is requested mid-write
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, b, err := ws.Read(ctx); err != nil || len(b) != len(big)+2 {
				t.Fatalf("in-flight message: %d bytes, %v", len(b), err)
			}
			_, _, err := ws.Read(ctx)
			var ce websocket.CloseError
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Reason != tc.reason {
				t.Fatalf("close: %v, want %v %q", err, tc.code, tc.reason)
			}
		})
	}
}
