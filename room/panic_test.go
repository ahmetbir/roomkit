package room_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
)

// panicSender panics on Send once armed: a stand-in for any bug on the room
// goroutine outside the game.
type panicSender struct {
	fakeSender
	armed atomic.Bool
}

func (p *panicSender) Send(v any) bool {
	if p.armed.Load() {
		panic("boom")
	}
	return p.fakeSender.Send(v)
}

// A panic inside the actor ends only that room: Done closes and every
// session is closed (spec §8).
func TestRoomPanicClosesRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{PanicStep: true}, nil)
		defer cancel()
		a, b := &fakeSender{}, &fakeSender{}
		if _, err := r.Join(t.Context(), room.Who{Name: "a"}, a); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Join(t.Context(), room.Who{Name: "b"}, b); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
		default:
			t.Fatal("room still running after a panic")
		}
		if !a.isClosed() || !b.isClosed() {
			t.Fatal("sessions left open after a panic")
		}
	})
}

// The same through a Sender that panics while the game sends a snapshot.
func TestSenderPanicClosesRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		bad := &panicSender{}
		other := &fakeSender{}
		if _, err := r.Join(t.Context(), room.Who{Name: "a"}, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Join(t.Context(), room.Who{Name: "b"}, other); err != nil {
			t.Fatal(err)
		}
		bad.armed.Store(true)
		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case <-r.Done():
		default:
			t.Fatal("room still running after a panic")
		}
		if !bad.isClosed() || !other.isClosed() {
			t.Fatal("sessions left open after a panic")
		}
	})
}

func TestPanickingGameStillClosesEverySeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := metrics.New("t", nil)
		r := room.New("ABCD", fakegame.New(fakegame.Settings{PanicStep: true, PanicClose: true}, nil), room.Options{Metrics: reg})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		out := &fakeSender{}
		done := make(chan struct{})
		go func() { defer close(done); r.Run(ctx) }()
		_, _ = r.Join(t.Context(), room.Who{Name: "a"}, out) // may race the first Step panic: either way the room ends
		<-done
		if !out.isClosed() && out.count("welcome") == 1 {
			t.Fatal("a seated session was not closed")
		}
		if reg.Humans.Load() != 0 || reg.Bots.Load() != 0 {
			t.Fatalf("gauges humans=%d bots=%d", reg.Humans.Load(), reg.Bots.Load())
		}
	})
}
