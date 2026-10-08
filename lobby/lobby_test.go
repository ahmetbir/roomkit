package lobby_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
)

type fakeLobby = lobby.Lobby[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]

func newLobby(ctx context.Context, maxRooms int, reg *metrics.Registry) *fakeLobby {
	return lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		MaxRooms: maxRooms, Metrics: reg,
		New: func(s fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			return fakegame.New(s, nil), nil
		},
	})
}

var plain = fakegame.Settings{Seats: 2}

type nopSender struct{}

func (nopSender) Send(any) bool { return true }
func (nopSender) Close()        {}

func TestCreateGetAndRemove(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 0, nil)
	r, err := l.Create(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lobby.NormalizeCode(r.Code()); !ok {
		t.Fatalf("bad code %q", r.Code())
	}
	got, ok := l.Get(strings.ToLower(r.Code()))
	if !ok || got != r {
		t.Fatal("Get(lowercase) did not find the room")
	}
	cancel()
	<-r.Done()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := l.Get(r.Code()); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("finished room still listed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCodesUnique(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 0, nil)
	seen := map[string]bool{}
	for range 20 {
		r, err := l.Create(plain)
		if err != nil {
			t.Fatal(err)
		}
		if seen[r.Code()] {
			t.Fatalf("duplicate code %s", r.Code())
		}
		seen[r.Code()] = true
	}
}

func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]string{"abcd": "ABCD", "Z29k": "Z29K", "HJNP": "HJNP"} {
		if got, ok := lobby.NormalizeCode(in); !ok || got != want {
			t.Errorf("NormalizeCode(%q)=%q,%v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "ABC", "ABCDE", "ABCI", "ABCO", "AB1D", "AB0D", "AB D", "ÂBCD", "../x"} {
		if _, ok := lobby.NormalizeCode(bad); ok {
			t.Errorf("NormalizeCode(%q) accepted", bad)
		}
	}
}

func TestMaxRooms(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 2, nil)
	var codes []string
	for range 2 {
		r, err := l.Create(plain)
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, r.Code())
	}
	if _, err := l.Create(plain); !errors.Is(err, lobby.ErrBusy) {
		t.Fatalf("3rd room: %v", err)
	}
	cancel()
	l.Wait()
	for _, c := range codes {
		if _, ok := l.Get(c); ok {
			t.Fatalf("stopped room %s must free its slot", c)
		}
	}
	// A shutting-down lobby starts nothing: a late Create must not race Wait.
	if _, err := l.Create(plain); !errors.Is(err, lobby.ErrBusy) {
		t.Fatalf("create after shutdown: %v", err)
	}
}

func TestListAndQuick(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 0, nil)
	listed := fakegame.Settings{Listed: true, Seats: 2}
	a, _ := l.Create(listed)
	b, _ := l.Create(listed)
	hidden, _ := l.Create(plain) // Listed false
	if _, err := b.Join(ctx, room.Who{Name: "x"}, nopSender{}); err != nil {
		t.Fatal(err)
	}
	got := l.List()
	if len(got) != 2 || got[0].Code != b.Code() || got[1].Code != a.Code() {
		t.Fatalf("list %+v", got)
	}
	for _, s := range got {
		if s.Code == hidden.Code() {
			t.Fatal("private rooms are not listed")
		}
	}
	if r, ok := l.Quick(); !ok || r != b {
		t.Fatal("quick picks the listed room with the most humans")
	}
	if _, err := b.Join(ctx, room.Who{Name: "y"}, nopSender{}); err != nil { // 2 seats: now full
		t.Fatal(err)
	}
	if r, ok := l.Quick(); !ok || r != a {
		t.Fatal("full rooms are skipped")
	}
}

// A NoQuick room is listed like any other but quick play never picks it,
// even when it is the busiest room with a free seat.
func TestQuickSkipsNoQuick(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 0, nil)
	open, _ := l.Create(fakegame.Settings{Listed: true, Seats: 1})
	noQuick, _ := l.Create(fakegame.Settings{Listed: true, Seats: 4, NoQuick: true})
	if _, err := noQuick.Join(ctx, room.Who{Name: "x"}, nopSender{}); err != nil {
		t.Fatal(err)
	}
	got := l.List()
	if len(got) != 2 || got[0].Code != noQuick.Code() || !got[0].NoQuick {
		t.Fatalf("list %+v", got)
	}
	if r, ok := l.Quick(); !ok || r != open {
		t.Fatal("quick must pass over the NoQuick room")
	}
	if r, ok := l.Get(noQuick.Code()); !ok || r != noQuick {
		t.Fatal("a NoQuick room stays reachable by code")
	}
	if _, err := open.Join(ctx, room.Who{Name: "y"}, nopSender{}); err != nil { // now full
		t.Fatal(err)
	}
	if r, ok := l.Quick(); ok {
		t.Fatalf("only the NoQuick room has a free seat: quick picked %s", r.Code())
	}
}

// A panic while building a room is an error: the code is freed and Wait
// does not hang on the room that never ran.
func TestBuildPanicIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 1, nil)
	if _, err := l.Create(fakegame.Settings{BadNew: true}); err == nil {
		t.Fatal("a panicking build must fail")
	}
	if _, err := l.Create(plain); err != nil { // the only slot is free again
		t.Fatalf("reservation kept: %v", err)
	}
	cancel()
	waited := make(chan struct{})
	go func() { l.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait hangs after a failed build")
	}
}

func TestFactoryErrorReleasesTheCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	l := lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		MaxRooms: 1,
		New: func(fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			calls++
			return nil, errors.New("no")
		},
	})
	if _, err := l.Create(fakegame.Settings{}); err == nil {
		t.Fatal("factory error must fail Create")
	}
	if _, err := l.Create(fakegame.Settings{}); err == nil || errors.Is(err, lobby.ErrBusy) || calls != 2 {
		t.Fatalf("the failed room must not hold the only slot: %v calls=%d", err, calls)
	}
	cancel()
	l.Wait()
}

// The rooms gauge follows rooms that run.
func TestRoomsGauge(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reg := metrics.New("dogfight", nil)
	l := newLobby(ctx, 0, reg)
	l.Create(plain)
	l.Create(plain)
	if reg.Rooms.Load() != 2 || reg.Bots.Load() != 4 { // an empty seat counts as a bot
		t.Fatalf("rooms %d bots %d", reg.Rooms.Load(), reg.Bots.Load())
	}
	cancel()
	l.Wait()
	if reg.Rooms.Load() != 0 || reg.Bots.Load() != 0 {
		t.Fatalf("after shutdown: rooms %d bots %d", reg.Rooms.Load(), reg.Bots.Load())
	}
}
