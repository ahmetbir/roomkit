package room

// outbox is the game's view of the seated humans during one call.
type outbox[M Msg[In], In Input[In], X any] struct{ r *Room[M, In, X] }

func (b outbox[M, In, X]) To(id PlayerID, v any) {
	if s, ok := b.r.sessions[id]; ok {
		s.out.Send(v)
	}
}

func (b outbox[M, In, X]) All(v any) {
	for _, s := range b.r.sessions {
		s.out.Send(v)
	}
}

func (b outbox[M, In, X]) Snap(v Acker) {
	for _, s := range b.r.sessions {
		s.out.Send(v.WithAck(s.q.ack))
	}
}

func (b outbox[M, In, X]) Changed() { b.r.publish() }
