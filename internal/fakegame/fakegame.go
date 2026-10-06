// Package fakegame is the smallest game the core's tests drive: players
// move a counter by their input's D, a snapshot of every counter goes out
// every second tick, and a "color" message changes the room summary. It has
// no bot logic (it reports every empty seat as a bot); a join fails once
// Seats humans sit, or always with Refuse's code when that is set.
package fakegame

import (
	"encoding/json"
	"fmt"

	"github.com/ahmetbir/roomkit/netproto"
	"github.com/ahmetbir/roomkit/room"
)

type Settings struct {
	Seats      int // default 4
	Listed     bool
	PanicStep  bool   // Step panics (room panic tests)
	PanicClose bool   // Close and Info panic too after a Step panic
	BadNew     bool   // New panics (lobby build tests)
	Refuse     string // non-empty: Join refuses everyone with this code
	Bots       int    // >0: the bots reported, whatever the humans (default: every empty seat)
}

// Input: D moves the player's counter, Shot is a one-shot press.
type Input struct {
	D    int  `json:"d"`
	Shot bool `json:"shot"`
}

func (i Input) Latch(d Input) Input { i.Shot = i.Shot || d.Shot; return i }
func (i Input) Held() Input         { i.Shot = false; return i }

// Msg is every client message of the fake game.
type Msg struct {
	T      string  `json:"t"`
	V      int     `json:"v,omitempty"`
	Name   string  `json:"name,omitempty"`
	Tok    string  `json:"tok,omitempty"`
	Code   string  `json:"code,omitempty"`
	Seq    uint32  `json:"seq,omitempty"`
	TS     float64 `json:"ts,omitempty"`
	Chat   int     `json:"id,omitempty"`
	D      int     `json:"d,omitempty"`
	Shot   bool    `json:"shot,omitempty"`
	Seats  int     `json:"seats,omitempty"`  // create
	Refuse string  `json:"refuse,omitempty"` // create: a room that refuses every join
	Color  string  `json:"color,omitempty"`  // "color": the game's own in-room message
}

func (m Msg) Head() netproto.Header {
	return netproto.Header{T: m.T, V: m.V, Name: m.Name, Tok: m.Tok, Code: m.Code, Seq: m.Seq, TS: m.TS, Chat: m.Chat}
}
func (m Msg) Input() Input    { return Input{D: m.D, Shot: m.Shot} }
func (m Msg) Latch(d Msg) Msg { m.Shot = m.Shot || d.Shot; return m }

// Decode is the fake game's decoder.
func Decode(b []byte) (Msg, error) {
	var m Msg
	if err := json.Unmarshal(b, &m); err != nil {
		return Msg{}, err
	}
	switch m.T {
	case netproto.THello, netproto.TCreate, netproto.TJoin, netproto.TQuick, netproto.TIn, netproto.TPing, netproto.TChat, "color":
	default:
		return Msg{}, fmt.Errorf("fakegame: unknown type %q", m.T)
	}
	return m, netproto.CheckHeader(m.Head(), 6)
}

// Snap is the fake snapshot.
type Snap struct {
	T    string         `json:"t"`
	Tick int            `json:"tick"`
	Ack  uint32         `json:"ack"`
	Pos  map[uint32]int `json:"pos"`
}

func (s Snap) WithAck(a uint32) any { s.Ack = a; return s }
func (Snap) Replaceable()           {}

// Info is the fake game's lobby summary.
type Info struct{ Color string }

// Recorder counts what the room asked of the game (tests read it after the
// room stopped or under synctest.Wait).
type Recorder struct{ Leaves, Flushes, Closes, Steps int }

type Game struct {
	s        Settings
	rec      *Recorder
	tick     int
	next     room.PlayerID
	humans   map[room.PlayerID]int // position per human
	color    string
	panicked bool
}

func New(s Settings, rec *Recorder) *Game {
	if s.BadNew {
		panic("fakegame: bad settings")
	}
	if s.Seats <= 0 {
		s.Seats = 4
	}
	if rec == nil {
		rec = &Recorder{}
	}
	return &Game{s: s, rec: rec, humans: map[room.PlayerID]int{}, color: "red"}
}

var _ room.Game[Msg, Input, Info] = (*Game)(nil)

func (g *Game) Join(who room.Who) (room.PlayerID, error) {
	if g.s.Refuse != "" {
		return 0, room.Refuse(g.s.Refuse)
	}
	if len(g.humans) == g.s.Seats {
		return 0, fmt.Errorf("fakegame: %w", room.ErrFull)
	}
	g.next++
	g.humans[g.next] = 0
	return g.next, nil
}

func (g *Game) Welcome(id room.PlayerID, code, tok string, out room.Outbox) {
	out.To(id, map[string]any{"t": "welcome", "you": id, "code": code, "tok": tok, "tick": g.tick})
}

func (g *Game) Leave(id room.PlayerID) { delete(g.humans, id); g.rec.Leaves++ }

func (g *Game) Handle(id room.PlayerID, m Msg, out room.Outbox) {
	if m.T == "color" && m.Color != "" {
		g.color = m.Color
		out.To(id, netproto.NewNotice("color_set", m.Color))
		out.Changed()
	}
}

func (g *Game) Step(inputs map[room.PlayerID]Input, out room.Outbox) {
	g.rec.Steps++
	if g.s.PanicStep {
		g.panicked = true
		panic("fakegame: step")
	}
	g.tick++
	for id, in := range inputs {
		g.humans[id] += in.D
	}
	if g.tick%2 == 0 {
		pos := make(map[uint32]int, len(g.humans))
		for id, p := range g.humans {
			pos[uint32(id)] = p
		}
		out.Snap(Snap{T: "snap", Tick: g.tick, Pos: pos})
	}
}

func (g *Game) ChatScope(room.PlayerID) func(room.PlayerID) bool {
	return func(room.PlayerID) bool { return true }
}

func (g *Game) Info() room.Info[Info] {
	if g.panicked && g.s.PanicClose {
		panic("fakegame: info")
	}
	return room.Info[Info]{Humans: len(g.humans), Seats: g.s.Seats, Bots: g.bots(), Listed: g.s.Listed, Game: Info{Color: g.color}}
}

func (g *Game) Label() string { return "fake" }
func (g *Game) FlushStats()   { g.rec.Flushes++ }

func (g *Game) Close() {
	g.rec.Closes++
	if g.panicked && g.s.PanicClose {
		panic("fakegame: close")
	}
}

func (g *Game) bots() int {
	if g.s.Bots > 0 {
		return g.s.Bots
	}
	return g.s.Seats - len(g.humans)
}
