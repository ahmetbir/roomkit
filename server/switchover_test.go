package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/pilot"
)

// The core's guarantees hold whatever the Kit answers: these tests plug in
// kits and stats that try to break them.

// hostileKit admits every message in a room, puts every type in the choice
// bucket and counts the times it was asked about a core type.
type hostileKit struct {
	fakeKit
	asked *atomic.Int64
}

func (k hostileKit) Class(t string) Class {
	if coreType(t) {
		k.asked.Add(1)
	}
	return ClassChoice
}

func (k hostileKit) InRoom(m fakegame.Msg) bool {
	if coreType(m.T) {
		k.asked.Add(1)
	}
	return true
}

func hostileServer(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	asked := &atomic.Int64{}
	srv := newServerWith(t, Options{}, 0, hostileKit{asked: asked}, nil)
	return srv.URL, asked
}

// Checklist (a): a Kit whose InRoom admits "hello" cannot reopen the
// handshake inside a room, and the core never asks it about core types.
func TestKitCannotReopenHandshakeInRoom(t *testing.T) {
	url, asked := hostileServer(t)
	for _, raw := range []string{`{"t":"hello","v":1}`, `{"t":"create","seats":2}`, `{"t":"join","code":"ABCD"}`, `{"t":"quick"}`} {
		c, _ := joined(t, url)
		c.send(`{"t":"ping","ts":1}`)
		c.send(`{"t":"chat","id":1}`)
		c.send(inMsg(1))
		c.until("pong", 2*time.Second, nil) // the core types still work
		c.send(raw)
		if code := c.errorCode(); code != "bad_msg" {
			t.Fatalf("%s in a room: code %q, want bad_msg", raw, code)
		}
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("the Kit was asked %d times about core types", n)
	}
}

// Checklist (a): a core type keeps its own bucket whatever class the Kit
// claims for it; only a game type takes the Kit's class.
func TestCoreTypesKeepTheirRateClass(t *testing.T) {
	s := &fakeServer{kit: hostileKit{asked: &atomic.Int64{}}}
	for _, typ := range []string{"hello", "create", "join", "quick", "in", "ping", "chat"} {
		if s.class(typ) != ClassAll {
			t.Fatalf("%s took the Kit's class", typ)
		}
	}
	if s.class("color") != ClassChoice {
		t.Fatal("a game type must take the Kit's class")
	}
	now := time.Unix(0, 0)
	g := newMsgGuard(Limits{MsgRate: 90, MsgBurst: 10, PickRate: 1, PickBurst: 9, PingRate: 1, PingBurst: 1}, func() time.Time { return now })
	g.check("ping", ClassChoice)
	if v, b := g.check("ping", ClassChoice); v != kick || b != "ping" {
		t.Fatalf("ping charged to %q (%v), want its own bucket", b, v)
	}
}

// countKit counts Decode calls and answers them with err (nil = decode).
type countKit struct {
	fakeKit
	decodes *int
	err     error
}

func (k countKit) Decode(b []byte) (fakegame.Msg, error) {
	*k.decodes++
	if k.err != nil {
		return fakegame.Msg{}, k.err
	}
	return fakegame.Decode(b)
}

// Checklist (b): the hard ceiling counts the raw frame before the game's
// decoder sees it.
func TestCeilingRunsBeforeDecode(t *testing.T) {
	now := time.Unix(100, 0)
	decodes := 0
	s := &fakeServer{kit: countKit{decodes: &decodes}}
	p := &peer[fakegame.Msg]{ip: "x", guard: newMsgGuard(Limits{}.withDefaults(), func() time.Time { return now })}
	if _, err := s.next(p, make([]byte, ceilBytes+1)); !errors.Is(err, errFlood) || decodes != 0 {
		t.Fatalf("oversize frame: %v, %d decodes", err, decodes)
	}
	p.guard = newMsgGuard(Limits{}.withDefaults(), func() time.Time { return now })
	for range ceilMsgs {
		s.next(p, []byte(inMsg(1)))
	}
	if _, err := s.next(p, []byte(inMsg(1))); !errors.Is(err, errFlood) || decodes != ceilMsgs {
		t.Fatalf("frame over the count: %v, %d decodes, want %d", err, decodes, ceilMsgs)
	}
}

// Checklist (c): whatever error the game's decoder returns, the client is
// told bad_msg, as Dogfight's decode errors always were.
func TestDecodeErrorsAreBadMsg(t *testing.T) {
	now := time.Unix(100, 0)
	for _, derr := range []error{errors.New("x"), context.DeadlineExceeded, errTimeout, errFlood, floodError{"all", "x"}} {
		decodes := 0
		s := &fakeServer{kit: countKit{decodes: &decodes, err: derr}, rejects: newRejectLog(time.Now, nil)}
		p := &peer[fakegame.Msg]{ip: "x", guard: newMsgGuard(Limits{}.withDefaults(), func() time.Time { return now })}
		_, err := s.next(p, []byte(`{"t":"in"}`))
		if msg := s.msgOf(err, p); msg != msgBad || errCode(msg) != "bad_msg" {
			t.Fatalf("decode error %v: %q", derr, msg)
		}
	}
}

// fakeStats records what the core hands the game's stats.
type fakeStats struct {
	mu     sync.Mutex
	hashes []string
	boards []string
	known  bool // Me's answer for every hash
}

func (*fakeStats) Ready() bool       { return true }
func (*fakeStats) Periods() []string { return []string{"week", "all"} }
func (f *fakeStats) Board(p string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boards = append(f.boards, p)
	return []byte(`{"period":"` + p + `"}`)
}
func (f *fakeStats) Me(hash string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hashes = append(f.hashes, hash)
	return []byte(`{"pilot":{"hash":"` + hash + `"}}`), f.known
}

func meReq(t *testing.T, url, tok string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url+"/api/me", nil)
	if tok != "" {
		req.Header.Set(pilot.Header, tok)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// Checklist (d): /api/me validates and hashes in the core; the game sees
// only the hash (the dummy's for a bad token), and a bad token is answered
// {"pilot":null} even when the game claims to know the pilot.
func TestMeHandsTheGameOnlyAHash(t *testing.T) {
	st := &fakeStats{known: true}
	srv := newServer(t, Options{Stats: st})
	good := pilot.New()
	for _, bad := range []string{"", "nope", strings.Repeat("A", 8192)} {
		if code, body := meReq(t, srv.URL, bad); code != 200 || body != `{"pilot":null}` {
			t.Fatalf("token %.20q: %d %s", bad, code, body)
		}
	}
	if code, body := meReq(t, srv.URL, good); code != 200 || body != `{"pilot":{"hash":"`+pilot.Hash(good)+`"}}` {
		t.Fatalf("good token: %d %s", code, body)
	}
	st.mu.Lock()
	st.known = false
	st.mu.Unlock()
	if code, body := meReq(t, srv.URL, good); code != 200 || body != `{"pilot":null}` {
		t.Fatalf("unknown pilot: %d %s", code, body)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	dummy := pilot.Hash(dummyToken)
	want := []string{dummy, dummy, dummy, pilot.Hash(good), pilot.Hash(good)}
	if strings.Join(st.hashes, ",") != strings.Join(want, ",") {
		t.Fatalf("hashes the game saw: %v, want %v", st.hashes, want)
	}
	for _, h := range st.hashes {
		if h == good || strings.Contains(h, "nope") {
			t.Fatal("a raw token reached the game")
		}
	}
}

// Checklist (e): ?period= is checked against Stats.Periods before the game
// builds (and echoes) a board.
func TestPeriodWhitelistedBeforeEcho(t *testing.T) {
	st := &fakeStats{}
	srv := newServer(t, Options{Stats: st})
	for _, bad := range []string{"", "year", "<script>", "week%00", "Week"} {
		if code, body := get(t, srv.URL+"/api/leaderboard?period="+bad); code != 400 || body != `{"code":"bad_period","error":"geçersiz dönem"}` {
			t.Fatalf("period %q: %d %s", bad, code, body)
		}
	}
	if code, body := get(t, srv.URL+"/api/leaderboard?period=all"); code != 200 || body != `{"period":"all"}` {
		t.Fatalf("all: %d %s", code, body)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if strings.Join(st.boards, ",") != "all" {
		t.Fatalf("boards built: %v", st.boards)
	}
}

func TestBurstOf(t *testing.T) {
	for in, want := range map[float64]int{0.5: 1, 1: 1, 2.2: 3, 3: 3, 10: 10} {
		if got := burstOf(in); got != want {
			t.Errorf("burstOf(%v)=%d want %d", in, got, want)
		}
	}
}

// A board the store could not answer (closed after Ready) is not cached:
// the next request rebuilds it instead of serving an empty board for boardTTL.
func TestAPICacheKeepsNoNilBody(t *testing.T) {
	var c apiCache
	now, builds := time.Unix(0, 0), 0
	build := func(b []byte) func() []byte { return func() []byte { builds++; return b } }
	if c.get(now, time.Minute, build(nil)) != nil {
		t.Fatal("nil body")
	}
	if string(c.get(now, time.Minute, build([]byte("x")))) != "x" || builds != 2 {
		t.Fatalf("nil body was cached (builds %d)", builds)
	}
	if string(c.get(now, time.Minute, build([]byte("y")))) != "x" || builds != 2 {
		t.Fatal("a real body is cached for ttl")
	}
}
