package wsconn

import (
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/internal/tsconst"
)

// The client core reconnects on the drain close code (CLOSE_RESTART in
// ts/net/socket.ts); Restart sends it.
func TestRestartCodeMatchesClientCore(t *testing.T) {
	got, err := tsconst.Int("net/socket.ts", "CLOSE_RESTART")
	if err != nil {
		t.Fatal(err)
	}
	if got != int(websocket.StatusServiceRestart) {
		t.Fatalf("CLOSE_RESTART = %d, Restart sends %d", got, websocket.StatusServiceRestart)
	}
}

// An idle client still pings (PING_MS in ts/net/socket.ts)
// often enough that the default idle timeout never closes it: at least
// twice per timeout, so one late ping is not fatal.
func TestPingIntervalUnderIdleTimeout(t *testing.T) {
	ms, err := tsconst.Int("net/socket.ts", "PING_MS")
	if err != nil {
		t.Fatal(err)
	}
	idle := Options{}.withDefaults().IdleTimeout
	if ping := time.Duration(ms) * time.Millisecond; 2*ping > idle {
		t.Fatalf("PING_MS %v: more than half the idle timeout %v", ping, idle)
	}
}
