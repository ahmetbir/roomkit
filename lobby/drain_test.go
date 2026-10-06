package lobby_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/room"
)

// A draining lobby (blue/green deploy) starts no room and quick-picks none;
// running rooms go on. Undrain restores both.
func TestDrainRefusesNewRoomsAndQuick(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := newLobby(ctx, 0, nil)
	r, err := l.Create(fakegame.Settings{Listed: true, Seats: 2})
	if err != nil {
		t.Fatal(err)
	}
	l.Drain(true)
	if _, err := l.Create(plain); !errors.Is(err, lobby.ErrDraining) {
		t.Fatalf("create while draining: %v, want ErrDraining", err)
	}
	if _, ok := l.Quick(); ok {
		t.Fatal("quick play picked a room while draining")
	}
	if got, ok := l.Get(r.Code()); !ok || got != r {
		t.Fatal("a running room vanished on drain")
	}
	l.Drain(false)
	if _, ok := l.Quick(); !ok {
		t.Fatal("quick play after undrain")
	}
	if _, err := l.Create(plain); err != nil {
		t.Fatalf("create after undrain: %v", err)
	}
}

func TestFlushStatsReachesEveryRoom(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	var recs []*fakegame.Recorder
	l := lobby.New(ctx, lobby.Options[fakegame.Settings, fakegame.Msg, fakegame.Input, fakegame.Info]{
		New: func(s fakegame.Settings) (room.Game[fakegame.Msg, fakegame.Input, fakegame.Info], error) {
			rec := &fakegame.Recorder{}
			mu.Lock()
			recs = append(recs, rec)
			mu.Unlock()
			return fakegame.New(s, rec), nil
		},
	})
	for range 3 {
		if _, err := l.Create(plain); err != nil {
			t.Fatal(err)
		}
	}
	fctx, fcancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer fcancel()
	if !l.FlushStats(fctx) {
		t.Fatal("rooms did not acknowledge the flush")
	}
	cancel()
	l.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 3 {
		t.Fatalf("%d games built", len(recs))
	}
	for i, rec := range recs {
		if rec.Flushes != 1 {
			t.Errorf("room %d flushed %d times", i, rec.Flushes)
		}
	}
}
