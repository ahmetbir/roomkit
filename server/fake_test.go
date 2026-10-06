package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/room"
)

// fakeKit plugs fakegame into the server for this package's internal tests.
// It copies core/internal/fakekit, which imports this package and so cannot
// be imported here: the one accepted duplicate.
type fakeKit struct{}

var _ Kit[fakegame.Settings, fakegame.Msg, fakegame.Info] = fakeKit{}

func (fakeKit) Version() int                          { return 1 }
func (fakeKit) Decode(b []byte) (fakegame.Msg, error) { return fakegame.Decode(b) }
func (fakeKit) QuickSettings(time.Time) fakegame.Settings {
	return fakegame.Settings{Seats: 2, Listed: true}
}
func (fakeKit) Class(t string) Class {
	if t == "color" {
		return ClassChoice
	}
	return ClassAll
}
func (fakeKit) InRoom(m fakegame.Msg) bool { return m.T == "color" }
func (fakeKit) Settings(m fakegame.Msg, _ time.Time) (fakegame.Settings, bool) {
	return fakegame.Settings{Seats: m.Seats, Listed: true}, m.Seats >= 1 && m.Seats <= 8
}
func (fakeKit) Row(s room.Summary[fakegame.Info]) any {
	return map[string]any{"code": s.Code, "humans": s.Humans, "seats": s.Seats, "color": s.Game.Color}
}

type fakeServer = Server[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]

type fakeLobby = lobby.Lobby[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]

func newFakeLobby(ctx context.Context, maxRooms int) *fakeLobby {
	return lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		MaxRooms: maxRooms,
		New: func(s fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			return fakegame.New(s, nil), nil
		},
	})
}

// newServerWith serves a fake-game server built over the lobby; the lobby's
// context ends with the test.
func newServerWith(t *testing.T, o Options, maxRooms int, k Kit[fakegame.Settings, fakegame.Msg, fakegame.Info],
	adjust func(*fakeServer, *fakeLobby)) *httptest.Server {
	t.Helper()
	if o.Limits == (Limits{}) { // tests share 127.0.0.1: lift the per-address caps
		o.Limits = Limits{MaxConnsIP: 1000, CreatePerMinIP: 1000, JoinFailPerMinIP: 1000, JoinPerMinIP: 1000}
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := newFakeLobby(ctx, maxRooms)
	s := New(l, k, o)
	if adjust != nil {
		adjust(s, l)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(func() { cancel(); srv.Close() })
	return srv
}

func newServer(t *testing.T, o Options) *httptest.Server {
	return newServerWith(t, o, 0, fakeKit{}, nil)
}

// tight returns limits that are generous except for the field under test.
func tight(f func(*Limits)) Limits {
	l := Limits{MaxConnsIP: 1000, CreatePerMinIP: 1000, JoinFailPerMinIP: 1000, JoinPerMinIP: 1000}
	f(&l)
	return l
}

var web = fstest.MapFS{
	"index.html": {Data: []byte("<!doctype html>fake")},
	"app.js":     {Data: []byte("console.log(1)")},
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func head(t *testing.T, url string) *http.Response {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

// rawDial dials /ws and returns the HTTP status on failure.
func rawDial(t *testing.T, url string, h http.Header) (*websocket.Conn, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ws, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if res == nil {
			t.Fatal(err)
		}
		return nil, res.StatusCode
	}
	t.Cleanup(func() { ws.CloseNow() })
	return ws, 101
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	ws, code := rawDial(t, url, nil)
	if ws == nil {
		t.Fatalf("dial: %d", code)
	}
	return &client{t, ws}
}

// writeRaw writes one text frame; false once the server has closed.
func (c *client) writeRaw(s string) bool {
	return c.ws.Write(c.t.Context(), websocket.MessageText, []byte(s)) == nil
}

func (c *client) send(s string) {
	c.t.Helper()
	if !c.writeRaw(s) {
		c.t.Fatal("write failed")
	}
}

// until reads messages until one of type typ satisfies ok, or fails after d.
func (c *client) until(typ string, d time.Duration, ok func([]byte) bool) []byte {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.t.Context(), d)
	defer cancel()
	for {
		_, b, err := c.ws.Read(ctx)
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", typ, err)
		}
		var h struct{ T string }
		json.Unmarshal(b, &h)
		if h.T == typ && (ok == nil || ok(b)) {
			return b
		}
	}
}

// closeStatus reads until the server closes and returns the close code.
func (c *client) closeStatus(d time.Duration) websocket.StatusCode {
	ctx, cancel := context.WithTimeout(c.t.Context(), d)
	defer cancel()
	for {
		if _, _, err := c.ws.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// errorCode reads until the server's error message and returns its code.
func (c *client) errorCode() string {
	c.t.Helper()
	var e struct{ Code string }
	json.Unmarshal(c.until("error", 5*time.Second, nil), &e)
	return e.Code
}

const fakeHello = `{"t":"hello","v":1,"name":"x"}`

// joined is a client seated in a new two-seat room; its code is returned.
func joined(t *testing.T, url string) (*client, string) {
	t.Helper()
	c := dial(t, url)
	c.send(fakeHello)
	c.send(`{"t":"create","seats":2}`)
	var w struct{ Code string }
	json.Unmarshal(c.until("welcome", 5*time.Second, nil), &w)
	return c, w.Code
}

func inMsg(seq int) string { return `{"t":"in","seq":` + strconv.Itoa(seq) + `,"d":1}` }
