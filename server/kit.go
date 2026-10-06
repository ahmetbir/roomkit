package server

import (
	"time"

	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/room"
)

// Msg is a decoded client message as the server sees it.
type Msg[M, In any] interface {
	room.Msg[In]
	Latch(dropped M) M // m plus the one-shot presses of an input dropped over its rate
}

// coreType reports whether t is a core message type: the handshake's
// (hello, create, join, quick) or the in-room core's (in, ping, chat). The
// server decides these itself and never asks the Kit's Class or InRoom
// about them, so no game can reopen the handshake inside a room or move a
// core type to another rate bucket.
func coreType(t string) bool {
	switch t {
	case netproto.THello, netproto.TCreate, netproto.TJoin, netproto.TQuick, netproto.TIn, netproto.TPing, netproto.TChat:
		return true
	}
	return false
}

// Class is the rate class of a game message type (core types have their own).
type Class uint8

const (
	ClassAll    Class = iota // only the connection's shared bucket
	ClassChoice              // the small choice bucket (pick-like; refusal kicks), then the shared one
)

// Kit is what a game plugs into the server. Its methods run on connection
// and HTTP goroutines: they must not touch shared state.
type Kit[S, M, X any] interface {
	Version() int                               // protocol version a hello must carry
	Decode(b []byte) (M, error)                 // one whole frame; the core reads Head()
	Settings(create M, now time.Time) (S, bool) // room settings of a create message; false = bad_room
	QuickSettings(now time.Time) S              // the room quick play makes when none is free
	Class(t string) Class                       // rate class of a game message type
	InRoom(m M) bool                            // a game message allowed after the handshake
	Row(s room.Summary[X]) any                  // one /api/rooms row
}

// Stats serves the pilot API; nil = stats off (503). Safe for concurrent use.
type Stats interface {
	Ready() bool
	Boards() []BoardID                  // the leaderboards served: the ?period=&key= whitelist
	Board(id BoardID) []byte            // body of an allowed board; nil = store closed
	Me(pilotHash string) ([]byte, bool) // the caller's body; false = unknown pilot
}

// BoardID names one leaderboard: GET /api/leaderboard?period=<Period>&key=<Key>
// (a missing key is ""). Key is the game's (Dogfight: none; a racing game:
// a track).
type BoardID struct{ Period, Key string }
