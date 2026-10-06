package room

import "github.com/ahmetbir/roomkit/netproto"

// chat relays preset id from a player to the game's chat scope. Chats inside
// the cooldown, or with an id outside 1..ChatMax, are dropped silently.
func (r *Room[M, In, X]) chat(from PlayerID, id int) {
	s, ok := r.sessions[from]
	if !ok || id < 1 || id > r.o.ChatMax || (s.chatAt != 0 && r.ticks-s.chatAt < r.o.ChatCooldown) {
		return
	}
	s.chatAt = max(1, r.ticks)
	to := r.game.ChatScope(from)
	msg := netproto.NewChat(from, id)
	for sid, o := range r.sessions {
		if to(sid) {
			o.out.Send(msg)
		}
	}
}
