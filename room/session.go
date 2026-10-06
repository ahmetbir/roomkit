package room

// session is one connected human, owned by the room goroutine.
type session[In Input[In]] struct {
	out    Sender
	q      queue[In]
	chatAt int // room tick of the last chat relayed; 0 = never
}

func newSession[In Input[In]](out Sender) *session[In] {
	return &session[In]{out: out, q: newQueue[In]()}
}
