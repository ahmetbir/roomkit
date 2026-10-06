package room_test

import (
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/room"
)

// The room's side of the Game contract: chat relay, summaries and the
// calls it hands to the game.

func TestChatCooldownAndScope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		a, b := &fakeSender{}, &fakeSender{}
		sa, _ := r.Join(t.Context(), room.Who{Name: "a"}, a)
		_, _ = r.Join(t.Context(), room.Who{Name: "b"}, b)
		sa.Input(fakegame.Msg{T: netproto.TChat, Chat: 1})
		sa.Input(fakegame.Msg{T: netproto.TChat, Chat: 2}) // inside 2 s: dropped
		sa.Input(fakegame.Msg{T: netproto.TChat, Chat: 9}) // over ChatMax: dropped
		time.Sleep(time.Second)
		synctest.Wait()
		if a.count("chat") != 1 || b.count("chat") != 1 {
			t.Fatalf("chat a=%d b=%d", a.count("chat"), b.count("chat"))
		}
		time.Sleep(2 * time.Second)
		sa.Input(fakegame.Msg{T: netproto.TChat, Chat: 3})
		time.Sleep(time.Second / 10)
		synctest.Wait()
		if b.count("chat") != 2 {
			t.Fatalf("after cooldown b=%d", b.count("chat"))
		}
	})
}

func TestChangedRepublishesSummary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{Listed: true}, nil)
		defer cancel()
		s, _ := r.Join(t.Context(), room.Who{Name: "a"}, &fakeSender{})
		if sum := r.Summary(); sum.Humans != 1 || sum.Code != "ABCD" || !sum.Listed || sum.Game.Color != "red" {
			t.Fatalf("%+v", sum)
		}
		s.Input(fakegame.Msg{T: "color", Color: "blue"})
		time.Sleep(time.Second / 120)
		synctest.Wait()
		if r.Summary().Game.Color != "blue" {
			t.Fatal("Changed did not republish")
		}
	})
}

// Join publishes before it replies and leave publishes too: the lobby never
// reads a stale seat count after either.
func TestJoinAndLeavePublish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		if r.Summary().Humans != 0 || r.Summary().Seats != 4 {
			t.Fatalf("initial %+v", r.Summary())
		}
		s, _ := r.Join(t.Context(), room.Who{Name: "a"}, &fakeSender{})
		if r.Summary().Humans != 1 { // no wait: published before the reply
			t.Fatalf("after join %+v", r.Summary())
		}
		s.Leave()
		synctest.Wait()
		if r.Summary().Humans != 0 {
			t.Fatalf("after leave %+v", r.Summary())
		}
	})
}

func TestFlushStatsAndLeaveReachTheGame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := &fakegame.Recorder{}
		r, cancel := start(t, fakegame.Settings{}, rec)
		s, _ := r.Join(t.Context(), room.Who{Name: "a"}, &fakeSender{})
		if !r.FlushStats(t.Context()) {
			t.Fatal("flush not acknowledged")
		}
		s.Leave()
		synctest.Wait()
		cancel()
		<-r.Done()
		if rec.Flushes != 1 || rec.Leaves != 1 || rec.Closes != 1 {
			t.Fatalf("%+v", *rec)
		}
		if !r.FlushStats(t.Context()) {
			t.Fatal("a stopped room reports flushed")
		}
	})
}

// The welcome a game sends carries the core envelope (netproto.Welcome):
// the client core rejoins with its code after a drain close.
func TestWelcomeCarriesTheCoreEnvelope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, cancel := start(t, fakegame.Settings{}, nil)
		defer cancel()
		out := &fakeSender{}
		s, err := r.Join(t.Context(), room.Who{Name: "a", NewToken: "tok-new"}, out)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		out.mu.Lock()
		defer out.mu.Unlock()
		for _, m := range out.msgs {
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var w netproto.Welcome
			if json.Unmarshal(b, &w) != nil || w.T != netproto.TWelcome {
				continue
			}
			if w.Code != "ABCD" || w.You != s.ID() || w.Tok != "tok-new" {
				t.Fatalf("welcome envelope %+v from %s", w, b)
			}
			return
		}
		t.Fatal("no welcome")
	})
}
