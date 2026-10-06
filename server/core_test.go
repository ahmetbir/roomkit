package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/internal/fakekit"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/room"
	"github.com/ahmetbir/roomkit/server"
)

type fakeServer = server.Server[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]

func newFake(t *testing.T, o server.Options, maxRooms int) (*httptest.Server, *fakeServer) {
	t.Helper()
	if o.Limits == (server.Limits{}) {
		o.Limits = server.Limits{MaxConnsIP: 1000, CreatePerMinIP: 1000, JoinFailPerMinIP: 1000, JoinPerMinIP: 1000}
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		MaxRooms: maxRooms,
		New: func(s fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			return fakegame.New(s, nil), nil
		},
	})
	s := server.New(l, fakekit.Kit{}, o)
	srv := httptest.NewServer(s)
	t.Cleanup(func() { cancel(); srv.Close() })
	return srv, s
}

func dialFake(t *testing.T, srv *httptest.Server, msgs ...string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	for _, m := range msgs {
		if err := ws.Write(t.Context(), websocket.MessageText, []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

// until reads frames until one of type typ; it returns that frame's JSON.
func until(t *testing.T, ws *websocket.Conn, typ string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		_, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if m["t"] == typ {
			return m
		}
	}
}

func TestFakeGameCreateJoinQuickAndPlay(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	a := dialFake(t, srv, `{"t":"hello","v":1,"name":"a"}`, `{"t":"create","seats":2}`)
	code := until(t, a, "welcome")["code"].(string)
	b := dialFake(t, srv, `{"t":"hello","v":1,"name":"b"}`, `{"t":"join","code":"`+strings.ToLower(code)+`"}`)
	until(t, b, "welcome")
	_ = b.Write(t.Context(), websocket.MessageText, []byte(`{"t":"in","seq":1,"d":5}`))
	_ = b.Write(t.Context(), websocket.MessageText, []byte(`{"t":"color","color":"blue"}`))
	until(t, b, "notice")
	c := dialFake(t, srv, `{"t":"hello","v":1,"name":"c"}`, `{"t":"quick"}`) // the room is full: quick creates
	if w := until(t, c, "welcome"); w["code"] == code {
		t.Fatal("quick joined a full room")
	}
	res, err := http.Get(srv.URL + "/api/rooms")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), `"color":"blue"`) || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("rooms: %s", body)
	}
}

func TestFakeGameRefusals(t *testing.T) {
	srv, s := newFake(t, server.Options{}, 0)
	for name, c := range map[string]struct {
		msgs []string
		code string
	}{
		"version":  {[]string{`{"t":"hello","v":2}`}, "version"},
		"badRoom":  {[]string{`{"t":"hello","v":1}`, `{"t":"create","seats":99}`}, "bad_room"},
		"noRoom":   {[]string{`{"t":"hello","v":1}`, `{"t":"join","code":"ZZZZ"}`}, "no_room"},
		"badFirst": {[]string{`{"t":"quick"}`}, "bad_msg"},
	} {
		ws := dialFake(t, srv, c.msgs...)
		if e := until(t, ws, "error"); e["code"] != c.code {
			t.Errorf("%s: %v", name, e)
		}
	}
	s.Drain(true)
	ws := dialFake(t, srv, `{"t":"hello","v":1}`, `{"t":"quick"}`)
	_, _, err := ws.Read(t.Context())
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.StatusServiceRestart {
		t.Fatalf("draining: %v", err)
	}
}

// A game's refusal reaches the player as an error frame whose code and text
// are the game's code; quick play skips a refusing room like a full one.
func TestFakeGameOwnRefusal(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	a := dialFake(t, srv, `{"t":"hello","v":1}`, `{"t":"create","seats":2,"refuse":"racing"}`)
	if e := until(t, a, "error"); e["code"] != "racing" || e["msg"] != "racing" {
		t.Fatalf("creator: %v", e)
	}
	res, err := http.Get(srv.URL + "/api/rooms")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var list struct{ Rooms []map[string]any }
	if err := json.Unmarshal(body, &list); err != nil || len(list.Rooms) != 1 {
		t.Fatalf("rooms: %s", body)
	}
	code := list.Rooms[0]["code"].(string)
	b := dialFake(t, srv, `{"t":"hello","v":1}`, `{"t":"join","code":"`+code+`"}`)
	if e := until(t, b, "error"); e["code"] != "racing" {
		t.Fatalf("join: %v", e)
	}
	c := dialFake(t, srv, `{"t":"hello","v":1}`, `{"t":"quick"}`)
	if w := until(t, c, "welcome"); w["code"] == code {
		t.Fatal("quick joined a refusing room")
	}
}

// A refusal code outside the protocol's shape, or one that is a core code,
// is a game bug: the player gets the core's full-room error instead.
func TestFakeGameBadRefusalCode(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	for _, bad := range []string{"Racing", "has space", "no_room", strings.Repeat("a", 33)} {
		raw, _ := json.Marshal(bad)
		ws := dialFake(t, srv, `{"t":"hello","v":1}`, `{"t":"create","seats":2,"refuse":`+string(raw)+`}`)
		if e := until(t, ws, "error"); e["code"] != "full" {
			t.Fatalf("%q: %v", bad, e)
		}
	}
}

func TestStatsOffIs503(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	for _, p := range []string{"/api/leaderboard?period=week", "/api/me"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 503 || !strings.Contains(string(b), `"stats_off"`) {
			t.Fatalf("%s: %d %s", p, res.StatusCode, b)
		}
	}
}
