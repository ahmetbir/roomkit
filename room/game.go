package room

import (
	"errors"

	"github.com/ahmetbir/roomkit/netproto"
)

// PlayerID is a seated player's id (uint32 on the wire).
type PlayerID = netproto.PlayerID

// Input is one tick of one player's controls.
type Input[In any] interface {
	Latch(dropped In) In // this input plus the one-shot presses of an older, dropped one
	Held() In            // this input without its one-shot presses (repeated while starved)
}

// Msg is a decoded client message: the core reads the header, the game the rest.
type Msg[In any] interface {
	Head() netproto.Header
	Input() In // the message as a tick input (T == "in")
}

// Outbox is how a game talks to the seated humans during one call. It must
// not be kept after the call returns.
type Outbox interface {
	To(id PlayerID, v any) // one seated human
	All(v any)             // every seated human
	Snap(v Acker)          // every seated human, each with its own input ack
	Changed()              // the game's Info changed: republish the lobby summary
}

// Acker is a snapshot that carries the receiving player's input ack.
type Acker interface{ WithAck(ack uint32) any }

// Who is the human asking for a seat.
type Who struct {
	Name     string
	Pilot    string // pilot.Hash(token); "" = not counted
	NewToken string // raw token to hand back in the welcome, only when just issued; never stored
}

// Info is what a game reports for the lobby.
type Info[X any] struct {
	Humans, Seats int // Seats is constant for a room's life; a seat without a human is a bot
	Listed        bool
	Game          X // game-specific summary; a value type (copied across goroutines)
}

// ErrFull: no seat is left. A game's Join wraps it.
var ErrFull = errors.New("room: full")

// Game is one match. The room calls every method on its own goroutine;
// none may block, start goroutines or keep the Outbox.
type Game[M Msg[In], In Input[In], X any] interface {
	Join(who Who) (PlayerID, error)
	Welcome(id PlayerID, code, newToken string, out Outbox) // sends id a message carrying netproto.Welcome's fields (you=id, code, tok=newToken)
	Leave(id PlayerID)
	Handle(id PlayerID, m M, out Outbox) // in-room messages other than in, ping and chat
	Step(inputs map[PlayerID]In, out Outbox)
	ChatScope(from PlayerID) func(to PlayerID) bool
	Info() Info[X]
	Label() string // for logs
	FlushStats()   // hand open tallies to the stats sink; players stay seated
	Close()        // the room stops: final tallies
}
