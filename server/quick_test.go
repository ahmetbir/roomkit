package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/room"
)

func TestQuickJoinsTheBusiestRoom(t *testing.T) {
	srv := newServer(t, Options{})
	a := dial(t, srv.URL)
	a.send(fakeHello)
	a.send(`{"t":"quick"}`)
	wa := a.until("welcome", 2*time.Second, nil)
	b := dial(t, srv.URL)
	b.send(fakeHello)
	b.send(`{"t":"quick"}`)
	wb := b.until("welcome", 2*time.Second, nil)
	var ca, cb struct{ Code string }
	json.Unmarshal(wa, &ca)
	json.Unmarshal(wb, &cb)
	if ca.Code == "" || ca.Code != cb.Code {
		t.Fatalf("both quick players must share the room: %q %q", ca.Code, cb.Code)
	}
}

// A full listed room is never picked: quick creates a new room.
func TestQuickSkipsFullListedRoom(t *testing.T) {
	srv := newServer(t, Options{})
	_, code := joined(t, srv.URL)
	second := dial(t, srv.URL)
	second.send(fakeHello)
	second.send(`{"t":"join","code":"` + code + `"}`)
	second.until("welcome", 2*time.Second, nil) // the listed room is now full
	q := dial(t, srv.URL)
	q.send(fakeHello)
	q.send(`{"t":"quick"}`)
	var got struct{ Code string }
	json.Unmarshal(q.until("welcome", 2*time.Second, nil), &got)
	if got.Code == "" || got.Code == code {
		t.Fatalf("quick must create a new room, got %q", got.Code)
	}
}

type nopSender struct{}

func (nopSender) Send(any) bool { return true }
func (nopSender) Close()        {}

type fakeRoom = room.Room[fakegame.Msg, fakegame.Input, fakegame.Info]

// quickServer is a server whose quick play picks with pick instead of
// the lobby's own Quick, so a test can race the picked room.
func quickServer(t *testing.T, lim Limits, pick func(l *fakeLobby) (*fakeRoom, bool)) (string, *fakeLobby) {
	t.Helper()
	var lb *fakeLobby
	srv := newServerWith(t, Options{Limits: lim}, 0, fakeKit{}, func(s *fakeServer, l *fakeLobby) {
		lb = l
		s.quickPick = func() (*fakeRoom, bool) { return pick(l) }
	})
	return srv.URL, lb
}

var listed2 = fakegame.Settings{Seats: 2, Listed: true}

func quickCode(t *testing.T, url string) string {
	t.Helper()
	q := dial(t, url)
	q.send(fakeHello)
	q.send(`{"t":"quick"}`)
	var got struct{ Code string }
	json.Unmarshal(q.until("welcome", 3*time.Second, nil), &got)
	return got.Code
}

// The picked room fills between the lobby's pick and the server's join:
// the player gets no error, a new room is made, and the join token is
// refunded.
func TestQuickFallsBackWhenPickedRoomFills(t *testing.T) {
	var picked *fakeRoom
	url, l := quickServer(t, tight(func(l *Limits) { l.JoinPerMinIP = 1 }), func(l *fakeLobby) (*fakeRoom, bool) {
		r, ok := l.Quick()
		if !ok {
			t.Error("the listed room with free seats was not picked")
			return r, ok
		}
		picked = r
		for { // other players take every seat after the pick
			if _, err := r.Join(context.Background(), room.Who{Name: "filler"}, nopSender{}); err != nil {
				break
			}
		}
		return r, ok
	})
	if _, err := l.Create(listed2); err != nil {
		t.Fatal(err)
	}
	code := quickCode(t, url)
	if picked == nil || code == "" || code == picked.Code() {
		t.Fatalf("quick must create a new room, got %q", code)
	}
	if s := picked.Summary(); s.Humans != s.Seats {
		t.Fatalf("the race did not fill the picked room: %+v", s)
	}
	// The failed quick join gave its token back: the address's only join
	// token still admits a join by code.
	j := dial(t, url)
	j.send(fakeHello)
	j.send(`{"t":"join","code":"` + code + `"}`)
	j.until("welcome", 3*time.Second, nil)
}

// The picked room stops between the pick and the join: same fallback.
func TestQuickFallsBackWhenPickedRoomCloses(t *testing.T) {
	gone := room.New("GONE", fakegame.New(listed2, nil), room.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	go gone.Run(ctx)
	cancel()
	<-gone.Done()
	url, _ := quickServer(t, tight(func(*Limits) {}), func(*fakeLobby) (*fakeRoom, bool) { return gone, true })
	if code := quickCode(t, url); code == "" || code == "GONE" {
		t.Fatalf("quick must create a new room, got %q", code)
	}
}
