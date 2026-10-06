package fakegame

import (
	"errors"
	"testing"

	"github.com/ahmetbir/roomkit/room"
)

type box struct {
	to, all, snaps []any
	changed        int
}

func (b *box) To(_ room.PlayerID, v any) { b.to = append(b.to, v) }
func (b *box) All(v any)                 { b.all = append(b.all, v) }
func (b *box) Snap(v room.Acker)         { b.snaps = append(b.snaps, v.WithAck(1)) }
func (b *box) Changed()                  { b.changed++ }

func TestFakeGameContract(t *testing.T) {
	g := New(Settings{Seats: 1}, nil)
	id, err := g.Join(room.Who{Name: "a"})
	if err != nil || id != 1 {
		t.Fatal(id, err)
	}
	if _, err := g.Join(room.Who{}); !errors.Is(err, room.ErrFull) {
		t.Fatalf("full: %v", err)
	}
	b := &box{}
	g.Step(map[room.PlayerID]Input{1: {D: 2}}, b)
	g.Step(map[room.PlayerID]Input{1: {D: 3}}, b)
	if len(b.snaps) != 1 || b.snaps[0].(Snap).Pos[1] != 5 || b.snaps[0].(Snap).Ack != 1 {
		t.Fatalf("%+v", b.snaps)
	}
	g.Handle(1, Msg{T: "color", Color: "blue"}, b)
	if b.changed != 1 || g.Info().Game.Color != "blue" {
		t.Fatal("color")
	}
}
