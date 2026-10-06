// Package fakekit plugs core/internal/fakegame into the server.
package fakekit

import (
	"time"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/room"
	"github.com/ahmetbir/roomkit/server"
)

type (
	Settings = fakegame.Settings
	Msg      = fakegame.Msg
	Info     = fakegame.Info
)

// Kit: create carries "seats"; the in-room game message is "color".
type Kit struct{}

var _ server.Kit[Settings, Msg, Info] = Kit{}

func (Kit) Version() int                     { return 1 }
func (Kit) Decode(b []byte) (Msg, error)     { return fakegame.Decode(b) }
func (Kit) QuickSettings(time.Time) Settings { return Settings{Seats: 2, Listed: true} }
func (Kit) Class(t string) server.Class {
	if t == "color" {
		return server.ClassChoice
	}
	return server.ClassAll
}
func (Kit) InRoom(m Msg) bool { return m.T == "color" }
func (Kit) Settings(m Msg, _ time.Time) (Settings, bool) {
	return Settings{Seats: m.Seats, Listed: true}, m.Seats >= 1 && m.Seats <= 8
}
func (Kit) Row(s room.Summary[Info]) any {
	return map[string]any{"code": s.Code, "humans": s.Humans, "seats": s.Seats, "color": s.Game.Color}
}
