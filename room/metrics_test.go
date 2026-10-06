package room_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
)

func TestRoomMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := metrics.New("dogfight", nil)
		r := room.New("MTRC", fakegame.New(fakegame.Settings{}, nil), room.Options{Metrics: reg})
		ctx, cancel := context.WithCancel(t.Context())
		go r.Run(ctx)
		if reg.Bots.Load() != 4 || reg.Humans.Load() != 0 {
			t.Fatalf("bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
		seat, _ := r.Join(ctx, room.Who{Name: "a"}, &fakeSender{})
		time.Sleep(time.Second)
		synctest.Wait()
		if reg.Bots.Load() != 3 || reg.Humans.Load() != 1 || reg.TickSeconds.Count() == 0 {
			t.Fatalf("bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
		seat.Leave()
		synctest.Wait()
		if reg.Bots.Load() != 4 || reg.Humans.Load() != 0 {
			t.Fatalf("after leave: bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
		cancel()
		<-r.Done()
		if reg.Bots.Load() != 0 || reg.Humans.Load() != 0 {
			t.Fatalf("a closed room leaves nothing behind: bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
	})
}

// A room closed with humans still seated gives back every gauge too.
func TestRoomMetricsClosedWithHumans(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := metrics.New("dogfight", nil)
		r := room.New("MTRH", fakegame.New(fakegame.Settings{}, nil), room.Options{Metrics: reg})
		ctx, cancel := context.WithCancel(t.Context())
		go r.Run(ctx)
		r.Join(ctx, room.Who{Name: "a"}, &fakeSender{})
		r.Join(ctx, room.Who{Name: "b"}, &fakeSender{})
		cancel()
		<-r.Done()
		if reg.Bots.Load() != 0 || reg.Humans.Load() != 0 {
			t.Fatalf("bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
	})
}

// The bot gauge is what the game reports (Info.Bots), not Seats-Humans: a
// grid of 2 bots in an 8-seat room stays 2 bots as humans come and go.
func TestRoomMetricsBotsAreReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := metrics.New("dogfight", nil)
		r := room.New("MTRB", fakegame.New(fakegame.Settings{Seats: 8, Bots: 2}, nil), room.Options{Metrics: reg})
		ctx, cancel := context.WithCancel(t.Context())
		go r.Run(ctx)
		if reg.Bots.Load() != 2 {
			t.Fatalf("bots %d", reg.Bots.Load())
		}
		r.Join(ctx, room.Who{Name: "a"}, &fakeSender{})
		synctest.Wait()
		if reg.Bots.Load() != 2 || reg.Humans.Load() != 1 {
			t.Fatalf("bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
		cancel()
		<-r.Done()
		if reg.Bots.Load() != 0 || reg.Humans.Load() != 0 {
			t.Fatalf("after close: bots %d humans %d", reg.Bots.Load(), reg.Humans.Load())
		}
	})
}
