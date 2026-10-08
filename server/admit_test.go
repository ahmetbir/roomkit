package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
)

// whoGame is the fake game reporting every Who its Join sees.
type whoGame struct {
	*fakegame.Game
	seen chan<- room.Who
}

func (g whoGame) Join(who room.Who) (room.PlayerID, error) {
	select {
	case g.seen <- who:
	default:
	}
	return g.Game.Join(who)
}

// admitKit is the fake kit with an Admitter: it refuses the name "blocked"
// with name_blocked and the name "bad" with a malformed code, and reports
// every request it is asked about.
type admitKit struct {
	fakeKit
	seen chan AdmitRequest
}

func (k admitKit) Admit(req AdmitRequest) (string, bool) {
	select {
	case k.seen <- req:
	default:
	}
	switch req.Who.Name {
	case "blocked":
		return "name_blocked", false
	case "bad":
		return "Not A Code", false
	}
	return "", true
}

// whoServer serves the fake game through k, its rooms reporting each
// joining Who on the returned channel.
func whoServer(t *testing.T, o Options, k Kit[fakegame.Settings, fakegame.Msg, fakegame.Info]) (string, <-chan room.Who) {
	t.Helper()
	seen := make(chan room.Who, 8)
	ctx, cancel := context.WithCancel(context.Background())
	l := lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		New: func(s fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			return whoGame{fakegame.New(s, nil), seen}, nil
		},
	})
	srv := httptest.NewServer(New(l, k, o))
	t.Cleanup(func() { cancel(); srv.Close() })
	return srv.URL, seen
}

func recvAdmit(t *testing.T, ch <-chan AdmitRequest) AdmitRequest {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("Admit not called")
		return AdmitRequest{}
	}
}

func recvWho(t *testing.T, ch <-chan room.Who) room.Who {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(3 * time.Second):
		t.Fatal("no Who reported")
		return room.Who{}
	}
}

// The handshake hands the game (and the Admitter) the address the
// per-address limits key on: X-Real-IP only from a trusted proxy. The
// address is never sent to the client.
func TestWhoCarriesTheClientAddress(t *testing.T) {
	lim := Limits{MaxConnsIP: 1000, CreatePerMinIP: 1000, JoinFailPerMinIP: 1000, JoinPerMinIP: 1000}
	loop := netip.MustParsePrefix("127.0.0.0/8")
	for name, c := range map[string]struct {
		trust []netip.Prefix
		want  string
	}{
		"trusted proxy":   {[]netip.Prefix{loop}, "198.51.100.7"},
		"untrusted proxy": {nil, "127.0.0.1"},
	} {
		t.Run(name, func(t *testing.T) {
			admits := make(chan AdmitRequest, 8)
			url, joins := whoServer(t, Options{Limits: lim, TrustProxy: c.trust}, admitKit{seen: admits})
			ws, code := rawDial(t, url, http.Header{"X-Real-IP": {"198.51.100.7"}})
			if ws == nil {
				t.Fatalf("dial: %d", code)
			}
			cl := &client{t, ws}
			cl.send(fakeHello)
			cl.send(`{"t":"create","seats":2}`)
			w := cl.until("welcome", 3*time.Second, nil)
			want := netip.MustParseAddr(c.want)
			if got := recvAdmit(t, admits).Who.Addr; got != want {
				t.Errorf("Admit saw %v, want %v", got, want)
			}
			if got := recvWho(t, joins).Addr; got != want {
				t.Errorf("Join saw %v, want %v", got, want)
			}
			if strings.Contains(string(w), c.want) {
				t.Errorf("the welcome carries the address: %s", w)
			}
		})
	}
}

// A Kit without an Admitter still hands the game the address.
func TestWhoAddrWithoutAdmitter(t *testing.T) {
	url, joins := whoServer(t, Options{Limits: tight(func(*Limits) {})}, fakeKit{})
	c := dial(t, url)
	c.send(fakeHello)
	c.send(`{"t":"quick"}`)
	c.until("welcome", 3*time.Second, nil)
	if got := recvWho(t, joins).Addr; got != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("Join saw %v", got)
	}
}

// refused dials, says hello as name, sends m and returns the error code.
func refused(t *testing.T, url, name, m string) string {
	t.Helper()
	c := dial(t, url)
	c.send(`{"t":"hello","v":1,"name":"` + name + `"}`)
	c.send(m)
	code := c.errorCode()
	if st := c.closeStatus(3 * time.Second); st != websocket.StatusPolicyViolation {
		t.Errorf("%s %s: close %v", name, m, st)
	}
	return code
}

// An Admit refusal comes before any room is picked, made or joined: no
// room is built, the room cap and the create and join tokens stay
// untouched, and a seated room keeps its seats.
func TestAdmitRefusalMakesNoRoom(t *testing.T) {
	var lb *fakeLobby
	srv := newServerWith(t, Options{Limits: tight(func(l *Limits) {
		l.CreatePerMinIP, l.JoinPerMinIP, l.JoinFailPerMinIP = 1, 1, 1
	})}, 1, admitKit{}, func(_ *fakeServer, l *fakeLobby) { lb = l })

	for _, m := range []string{`{"t":"create","seats":2}`, `{"t":"quick"}`, `{"t":"create","seats":2}`, `{"t":"quick"}`} {
		if code := refused(t, srv.URL, "blocked", m); code != "name_blocked" {
			t.Fatalf("%s: code %q", m, code)
		}
	}
	if n := len(lb.List()); n != 0 {
		t.Fatalf("refusals built %d rooms", n)
	}
	// The address's only create token and the server's only room slot
	// are still free.
	_, code := joined(t, srv.URL)
	r, ok := lb.Get(code)
	if !ok {
		t.Fatal("no room")
	}
	// A listed room with a free seat exists now: quick and join-by-code
	// are refused before it is touched, and the cap (full) is never reached.
	for _, m := range []string{`{"t":"quick"}`, `{"t":"join","code":"` + code + `"}`, `{"t":"join","code":"ZZZZ"}`} {
		if got := refused(t, srv.URL, "blocked", m); got != "name_blocked" {
			t.Fatalf("%s: code %q", m, got)
		}
	}
	if s := r.Summary(); s.Humans != 1 {
		t.Fatalf("refused joins took seats: %+v", s)
	}
	// The address's only join token was never spent, nor a join failure counted.
	j := dial(t, srv.URL)
	j.send(fakeHello)
	j.send(`{"t":"join","code":"` + code + `"}`)
	j.until("welcome", 3*time.Second, nil)
}

// An Admit refusal with a code outside the protocol's shape is a game bug:
// the player gets the core's full-room error, and still no room is made.
func TestAdmitBadCode(t *testing.T) {
	var lb *fakeLobby
	srv := newServerWith(t, Options{}, 0, admitKit{}, func(_ *fakeServer, l *fakeLobby) { lb = l })
	for _, m := range []string{`{"t":"create","seats":2}`, `{"t":"quick"}`} {
		if code := refused(t, srv.URL, "bad", m); code != "full" {
			t.Fatalf("%s: code %q", m, code)
		}
	}
	if n := len(lb.List()); n != 0 {
		t.Fatalf("refusals built %d rooms", n)
	}
}

// Quick play passes over a NoQuick room; /api/rooms still lists it and a
// join by code still seats a player there.
func TestQuickSkipsNoQuickRoom(t *testing.T) {
	var lb *fakeLobby
	srv := newServerWith(t, Options{}, 0, fakeKit{}, func(_ *fakeServer, l *fakeLobby) { lb = l })
	nq, err := lb.Create(fakegame.Settings{Seats: 2, Listed: true, NoQuick: true})
	if err != nil {
		t.Fatal(err)
	}
	code := quickCode(t, srv.URL)
	if code == "" || code == nq.Code() {
		t.Fatalf("quick must make a new room, got %q", code)
	}
	_, body := get(t, srv.URL+"/api/rooms")
	var list struct{ Rooms []struct{ Code string } }
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, r := range list.Rooms {
		codes = append(codes, r.Code)
	}
	if len(codes) != 2 || !strings.Contains(strings.Join(codes, ","), nq.Code()) {
		t.Fatalf("rooms: %s", body)
	}
	j := dial(t, srv.URL)
	j.send(fakeHello)
	j.send(`{"t":"join","code":"` + nq.Code() + `"}`)
	j.until("welcome", 3*time.Second, nil)
}

// Admit learns what is asked: the kind, and for a join the room code
// (upper-cased when well formed, as sent otherwise).
func TestAdmitRequestKindAndCode(t *testing.T) {
	seen := make(chan AdmitRequest, 8)
	srv := newServerWith(t, Options{}, 0, admitKit{seen: seen}, nil)
	_, code := joined(t, srv.URL)
	if r := recvAdmit(t, seen); r.Kind != "create" || r.Code != "" || r.Who.Name != "x" || r.Who.Pilot == "" {
		t.Fatalf("create: %+v", r)
	}
	for _, c := range []struct{ msg, kind, code string }{
		{`{"t":"quick"}`, "quick", ""},
		{`{"t":"join","code":"` + strings.ToLower(code) + `"}`, "join", code},
		{`{"t":"join","code":"zz"}`, "join", "zz"},
	} {
		cl := dial(t, srv.URL)
		cl.send(`{"t":"hello","v":1,"name":"blocked"}`)
		cl.send(c.msg)
		cl.errorCode()
		if r := recvAdmit(t, seen); r.Kind != c.kind || r.Code != c.code {
			t.Fatalf("%s: %+v", c.msg, r)
		}
	}
}

// A draining server answers 1012 before asking Admit; a second message
// that is not create, quick or join is bad_msg without asking Admit.
func TestAdmitNotAskedWhenDrainingOrMalformed(t *testing.T) {
	seen := make(chan AdmitRequest, 8)
	var srvr *fakeServer
	srv := newServerWith(t, Options{}, 0, admitKit{seen: seen}, func(s *fakeServer, _ *fakeLobby) { srvr = s })
	for _, m := range []string{`{"t":"hello","v":1,"name":"blocked"}`, `{"t":"color","color":"red"}`, `{"t":"in","seq":1}`} {
		c := dial(t, srv.URL)
		c.send(`{"t":"hello","v":1,"name":"blocked"}`)
		c.send(m)
		if code := c.errorCode(); code != "bad_msg" {
			t.Fatalf("%s: %q", m, code)
		}
	}
	srvr.Drain(true)
	for _, m := range []string{`{"t":"create","seats":2}`, `{"t":"quick"}`, `{"t":"join","code":"ZZZZ"}`} {
		c := dial(t, srv.URL)
		c.send(`{"t":"hello","v":1,"name":"blocked"}`)
		c.send(m)
		if st := c.closeStatus(3 * time.Second); st != websocket.StatusServiceRestart {
			t.Fatalf("draining %s: close %v", m, st)
		}
	}
	select {
	case r := <-seen:
		t.Fatalf("Admit asked: %+v", r)
	default:
	}
}

// Every Admit refusal counts as the "admit" reject; the log line names the
// code (or full for a bad one) and the kind, never the player's name.
func TestAdmitRefusalCountedAndLogged(t *testing.T) {
	var buf syncBuf
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	reg := metrics.New("rk", nil)
	srv := newServerWith(t, Options{Metrics: reg}, 0, admitKit{}, nil)
	refused(t, srv.URL, "blocked", `{"t":"quick"}`)
	refused(t, srv.URL, "blocked", `{"t":"create","seats":2}`)
	refused(t, srv.URL, "bad", `{"t":"create","seats":2}`)
	var out strings.Builder
	if err := reg.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `rk_rejects_total{reason="admit"} 3`) {
		t.Fatalf("metrics:\n%s", out.String())
	}
	log := buf.String()
	for _, want := range []string{"reason=admit", "code=name_blocked kind=quick", "code=full kind=create"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "blocked\"") || strings.Contains(log, "name=") {
		t.Errorf("log names the player:\n%s", log)
	}
}

// syncBuf is a bytes.Buffer safe for the server's goroutines.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
