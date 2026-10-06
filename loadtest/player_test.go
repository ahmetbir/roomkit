package loadtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type script struct{ reacted atomic.Int32 }

func (*script) Hello(i int) any             { return map[string]any{"t": "hello", "v": 1, "name": "p"} }
func (*script) Create() any                 { return map[string]any{"t": "create", "seats": 2} }
func (*script) Input(i int, seq uint32) any { return map[string]any{"t": "in", "seq": seq} }
func (s *script) React(i, you int, t string, raw []byte) any {
	if t == "roster" {
		s.reacted.Add(1)
		return map[string]any{"t": "color", "color": "blue"}
	}
	return nil
}

// wireLog records the entry messages and the first ping the server saw.
type wireLog struct {
	mu      sync.Mutex
	entries []string
	ping    string
}

func (w *wireLog) entry(b []byte) {
	w.mu.Lock()
	w.entries = append(w.entries, string(b))
	w.mu.Unlock()
}

func (w *wireLog) firstPing(b []byte) {
	w.mu.Lock()
	if w.ping == "" {
		w.ping = string(b)
	}
	w.mu.Unlock()
}

// echoServer: welcome, one roster, snapshots at 30 Hz, pongs; counts colors
// and records entries and pings in wire (nil = not recorded).
func echoServer(t *testing.T, colors *atomic.Int32, wire *wireLog) *httptest.Server {
	if wire == nil {
		wire = &wireLog{}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		ws.Read(ctx) // hello
		_, entry, err := ws.Read(ctx)
		if err != nil {
			return
		}
		wire.entry(entry)
		ws.Write(ctx, websocket.MessageText, []byte(`{"t":"welcome","you":1,"code":"ABCD"}`))
		ws.Write(ctx, websocket.MessageText, []byte(`{"t":"roster"}`))
		go func() {
			for tick := 2; ctx.Err() == nil; tick += 2 {
				ws.Write(ctx, websocket.MessageText, []byte(`{"t":"snap","tick":`+itoa(tick)+`}`))
				time.Sleep(33 * time.Millisecond)
			}
		}()
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			json.Unmarshal(b, &m)
			switch m["t"] {
			case "ping":
				wire.firstPing(b)
				ws.Write(ctx, websocket.MessageText, []byte(`{"t":"pong","ts":`+itoa(int(m["ts"].(float64)))+`}`))
			case "color":
				colors.Add(1)
			}
		}
	}))
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestRunDrivesAScript(t *testing.T) {
	var colors atomic.Int32
	srv := echoServer(t, &colors, nil)
	defer srv.Close()
	sc := &script{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Run(ctx, Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Players: 2, Rooms: 1, InputHz: 60, SnapEvery: 2,
		Duration: 1500 * time.Millisecond, Ramp: 100 * time.Millisecond, Every: 500 * time.Millisecond}, sc); err != nil {
		t.Fatal(err)
	}
	if sc.reacted.Load() != 2 || colors.Load() != 2 {
		t.Fatalf("reacted=%d colors=%d", sc.reacted.Load(), colors.Load())
	}
}

// The game's cadence has no default.
func TestRunNeedsTheCadence(t *testing.T) {
	for _, c := range []Config{{SnapEvery: 2}, {InputHz: 60}} {
		if err := Run(t.Context(), c, &script{}); err == nil {
			t.Fatalf("%+v: no error", c)
		}
	}
}

// A gap is a snapshot lost: ticks further apart than the game's snapshot step.
func TestGapsFollowSnapEvery(t *testing.T) {
	for _, c := range []struct {
		d, every int
		want     uint64
	}{{2, 2, 0}, {4, 2, 1}, {5, 2, 1}, {6, 2, 2}, {1, 1, 0}, {2, 1, 1}, {4, 1, 3}, {0, 2, 0}, {-3, 1, 0}} {
		if got := gaps(c.d, c.every); got != c.want {
			t.Errorf("gaps(%d, %d) = %d, want %d", c.d, c.every, got, c.want)
		}
	}
}

func TestRoomCodeHandsOverOnce(t *testing.T) {
	c := newRoomCode()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, ok := c.wait(ctx); ok {
		t.Fatal("wait returned before set")
	}
	c.set("ABCD")
	c.set("ZZZZ")
	if code, ok := c.wait(context.Background()); !ok || code != "ABCD" {
		t.Errorf("wait = %q, %v; want ABCD", code, ok)
	}
}

// The entry and ping messages are anonymous structs in the player loop; their
// JSON is what protocol.ClientMsg produced before the extraction (join and
// quick carry only t [+ code], ping t + ts), so it is pinned. (The create
// entry is the Script's own message.)
func TestEntryAndPingJSON(t *testing.T) {
	run := func(players, rooms int) (entries []string, ping string) {
		var colors atomic.Int32
		w := &wireLog{}
		srv := echoServer(t, &colors, w)
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		Run(ctx, Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Players: players, Rooms: rooms, InputHz: 60, SnapEvery: 2,
			Duration: 1500 * time.Millisecond, Ramp: 100 * time.Millisecond, Every: time.Second}, &script{})
		w.mu.Lock()
		defer w.mu.Unlock()
		return append([]string(nil), w.entries...), w.ping
	}
	pingRe := regexp.MustCompile(`^\{"t":"ping","ts":[0-9]+(\.[0-9]+)?\}$`)

	entries, ping := run(2, 1)
	got := map[string]int{}
	for _, e := range entries {
		got[e]++
	}
	if got[`{"seats":2,"t":"create"}`] != 1 || got[`{"t":"join","code":"ABCD"}`] != 1 || len(entries) != 2 {
		t.Errorf("room entries = %q; want one create and join {\"t\":\"join\",\"code\":\"ABCD\"}", entries)
	}
	if !pingRe.MatchString(ping) {
		t.Errorf("ping = %q; want {\"t\":\"ping\",\"ts\":<ms>}", ping)
	}

	quick, _ := run(1, 0)
	if len(quick) != 1 || quick[0] != `{"t":"quick"}` {
		t.Errorf("quick entries = %q; want [{\"t\":\"quick\"}]", quick)
	}
}
