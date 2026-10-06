// Package netproto is the part of the wire protocol every game shares: the
// message envelope, the core message types (handshake, ping, chat, errors,
// notices), the welcome envelope and the core error codes. A game's own
// messages are flat JSON objects with the same "t" field; its snapshot
// starts {"t":"snap","tick":N; its welcome carries the Welcome fields.
package netproto

import (
	"errors"
	"math"
)

// PlayerID is a seated player's id on the wire.
type PlayerID uint32

// Header is what the core reads of a client message.
type Header struct {
	T    string  // message type
	V    int     // hello: protocol version
	Name string  // hello
	Tok  string  // hello: pilot token
	Code string  // join: room code
	Seq  uint32  // in: input sequence, from 1
	TS   float64 // ping: client timestamp, echoed in the pong
	Chat int     // chat: preset id, 1..chatMax
}

// Core client message types.
const (
	THello  = "hello"
	TCreate = "create"
	TJoin   = "join"
	TQuick  = "quick"
	TIn     = "in"
	TPing   = "ping"
	TChat   = "chat"
)

// TWelcome is the type of the message that seats a player.
const TWelcome = "welcome"

// Welcome is the envelope every game's welcome (room.Game.Welcome) carries,
// flat beside the game's own fields: {"t":"welcome","you":…,"code":…}. It is
// a core contract, not a game choice: the client core keeps Code to rejoin
// the same room after a 1012 (drain) close, and the load test reads Code
// (joiners) and You. A game that renames them loses reconnect-after-deploy
// silently. The TS twin is Welcome in ts/net/socket.ts.
type Welcome struct {
	T    string   `json:"t"`             // "welcome"
	You  PlayerID `json:"you"`           // the player's id in this room
	Code string   `json:"code"`          // the room's code (join, rejoin)
	Tok  string   `json:"tok,omitempty"` // a pilot token, only when just issued (room.Who.NewToken)
}

// Pong answers a ping, echoing its timestamp.
type Pong struct {
	T  string  `json:"t"` // "pong"
	TS float64 `json:"ts"`
}

// ErrorMsg ends the connection; Code names the failure, Msg is its (Turkish) text.
type ErrorMsg struct {
	T    string `json:"t"` // "error"
	Msg  string `json:"msg"`
	Code string `json:"code,omitempty"`
}

// NoticeMsg is a short, non-fatal message for the player.
type NoticeMsg struct {
	T    string `json:"t"` // "notice"
	Msg  string `json:"msg"`
	Code string `json:"code,omitempty"`
}

// ChatMsg relays quick chat preset ID (1..ChatMax, the game's preset count; CheckHeader's chatMax) from player From.
type ChatMsg struct {
	T    string   `json:"t"` // "chat"
	From PlayerID `json:"from"`
	ID   int      `json:"id"`
}

// NewPong answers a ping with its timestamp.
func NewPong(ts float64) Pong { return Pong{T: "pong", TS: ts} }

// NewError builds the fatal error message for a failure code.
func NewError(code, msg string) ErrorMsg { return ErrorMsg{T: "error", Msg: msg, Code: code} }

// NewNotice builds a non-fatal notice for the player.
func NewNotice(code, msg string) NoticeMsg { return NoticeMsg{T: "notice", Msg: msg, Code: code} }

// NewChat builds the relay of quick chat preset id from player from.
func NewChat(from PlayerID, id int) ChatMsg { return ChatMsg{T: "chat", From: from, ID: id} }

var (
	ErrNotFinite = errors.New("protocol: non-finite number")
	ErrBadChat   = errors.New("protocol: chat id out of range")
)

// CheckHeader applies the checks every game's decoder needs: a finite ping
// timestamp and a chat preset in 1..chatMax.
func CheckHeader(h Header, chatMax int) error {
	if h.T == TChat && (h.Chat < 1 || h.Chat > chatMax) {
		return ErrBadChat
	}
	if math.IsNaN(h.TS) || math.IsInf(h.TS, 0) {
		return ErrNotFinite
	}
	return nil
}
