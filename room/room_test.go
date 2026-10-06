package room_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/room"
)

type fakeSender struct {
	mu     sync.Mutex
	msgs   []any
	closed bool
}

func (f *fakeSender) Send(v any) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, v)
	return true
}
func (f *fakeSender) Close() { f.mu.Lock(); defer f.mu.Unlock(); f.closed = true }

func (f *fakeSender) count(t string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.msgs {
		switch v := m.(type) {
		case fakegame.Snap:
			if t == "snap" {
				n++
			}
		case netproto.Pong:
			if t == "pong" {
				n++
			}
		case netproto.ChatMsg:
			if t == "chat" {
				n++
			}
		case netproto.NoticeMsg:
			if t == "notice" {
				n++
			}
		case map[string]any:
			if v["t"] == t {
				n++
			}
		}
	}
	return n
}

func (f *fakeSender) lastSnap() (fakegame.Snap, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.msgs) - 1; i >= 0; i-- {
		if s, ok := f.msgs[i].(fakegame.Snap); ok {
			return s, true
		}
	}
	return fakegame.Snap{}, false
}

func (f *fakeSender) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

type fakeRoom = room.Room[fakegame.Msg, fakegame.Input, fakegame.Info]

func start(t *testing.T, s fakegame.Settings, rec *fakegame.Recorder) (*fakeRoom, context.CancelFunc) {
	r := room.New("ABCD", fakegame.New(s, rec), room.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	go r.Run(ctx)
	return r, cancel
}

func TestJoinReceivesWelcomeAndSnaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		out := &fakeSender{}
		seat, err := r.Join(t.Context(), room.Who{Name: "a"}, out)
		if err != nil || seat.ID() == 0 {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if out.count("welcome") != 1 || out.count("snap") < 25 {
			t.Fatalf("welcome=%d snaps=%d", out.count("welcome"), out.count("snap"))
		}
	})
}

func TestAckAdvances(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		out := &fakeSender{}
		seat, err := r.Join(t.Context(), room.Who{Name: "a"}, out)
		if err != nil {
			t.Fatal(err)
		}
		for seq := uint32(1); seq <= 10; seq++ {
			seat.Input(fakegame.Msg{T: netproto.TIn, Seq: seq, D: 1})
		}
		seat.Input(fakegame.Msg{T: netproto.TIn, Seq: 3, D: 1}) // stale: ignored
		time.Sleep(time.Second)
		synctest.Wait()
		if s, ok := out.lastSnap(); !ok || s.Ack != 10 {
			t.Fatalf("ack=%d", s.Ack)
		}
	})
}

func TestPingAndUnknownGameMessage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		out := &fakeSender{}
		seat, _ := r.Join(t.Context(), room.Who{Name: "a"}, out)
		seat.Input(fakegame.Msg{T: netproto.TPing, TS: 5})
		seat.Input(fakegame.Msg{T: "color"}) // empty color: the game ignores it
		time.Sleep(time.Second)
		synctest.Wait()
		if out.count("pong") != 1 || out.count("notice") != 0 {
			t.Fatalf("pong=%d notice=%d", out.count("pong"), out.count("notice"))
		}
		select {
		case <-r.Done():
			t.Fatal("room died")
		default:
		}
	})
}

func TestEmptyRoomCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		seat, err := r.Join(t.Context(), room.Who{Name: "a"}, &fakeSender{})
		if err != nil {
			t.Fatal(err)
		}
		seat.Leave()
		time.Sleep(room.DefaultEmptyTimeout - time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
			t.Fatal("closed too early")
		default:
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
		default:
			t.Fatal("empty room still running")
		}
		if _, err := r.Join(t.Context(), room.Who{Name: "b"}, &fakeSender{}); !errors.Is(err, room.ErrClosed) {
			t.Fatalf("join closed room: %v", err)
		}
		seat.Leave()                              // must not block
		seat.Input(fakegame.Msg{T: netproto.TIn}) // must not block
	})
}

func TestCancelClosesSessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		out := &fakeSender{}
		if _, err := r.Join(t.Context(), room.Who{Name: "a"}, out); err != nil {
			t.Fatal(err)
		}
		cancel()
		synctest.Wait()
		<-r.Done()
		if !out.isClosed() {
			t.Fatal("session not closed on shutdown")
		}
	})
}

func TestFullRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{Seats: 1}, nil)
		defer cancel()
		if _, err := r.Join(t.Context(), room.Who{Name: "p"}, &fakeSender{}); err != nil {
			t.Fatalf("join: %v", err)
		}
		if _, err := r.Join(t.Context(), room.Who{Name: "p"}, &fakeSender{}); !errors.Is(err, room.ErrFull) {
			t.Fatalf("2nd join: %v", err)
		}
	})
}

// A room nobody ever joins closes after UnusedTimeout, not EmptyTimeout.
func TestUnusedRoomCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		time.Sleep(room.DefaultUnusedTimeout - time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
			t.Fatal("closed too early")
		default:
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
		default:
			t.Fatal("unused room still running")
		}
	})
}
