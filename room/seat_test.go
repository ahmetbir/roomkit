package room

import (
	"testing"

	"github.com/ahmetbir/roomkit/netproto"
)

// stubMsg and stubGame are the least a Room needs to be built; the room's
// behaviour tests run on core/internal/fakegame (package room_test), which
// an in-package test cannot import.
type stubMsg struct{ T string }

func (m stubMsg) Head() netproto.Header { return netproto.Header{T: m.T} }
func (m stubMsg) Input() in             { return in{} }

type stubGame struct{}

func (stubGame) Join(Who) (PlayerID, error)               { return 1, nil }
func (stubGame) Welcome(PlayerID, string, string, Outbox) {}
func (stubGame) Leave(PlayerID)                           {}
func (stubGame) Handle(PlayerID, stubMsg, Outbox)         {}
func (stubGame) Step(map[PlayerID]in, Outbox)             {}
func (stubGame) ChatScope(PlayerID) func(PlayerID) bool   { return nil }
func (stubGame) Info() Info[struct{}]                     { return Info[struct{}]{Seats: 4} }
func (stubGame) Label() string                            { return "stub" }
func (stubGame) FlushStats()                              {}
func (stubGame) Close()                                   {}

// One flooding seat holds at most seatInbox slots of the shared queue, so
// another seat's input still gets in.
func TestSeatInboxBounded(t *testing.T) {
	r := New[stubMsg, in, struct{}]("ABCD", stubGame{}, Options{}) // not running: nothing drains the queue
	seat := func(id PlayerID) *Seat[stubMsg] {
		return &Seat[stubMsg]{id: id, slots: make(chan struct{}, seatInbox), inputs: r.inputs, leaves: r.leaves, done: r.done}
	}
	a, b := seat(1), seat(2)
	for range 10 * inputQueue {
		a.Input(stubMsg{T: netproto.TIn})
	}
	if len(r.inputs) != seatInbox {
		t.Fatalf("flooding seat queued %d, want %d", len(r.inputs), seatInbox)
	}
	b.Input(stubMsg{T: netproto.TIn})
	if len(r.inputs) != seatInbox+1 {
		t.Fatal("second seat starved")
	}
}
