package room

// Summary is what the lobby lists of a room: published by the actor,
// readable from any goroutine.
type Summary[X any] struct {
	Code string
	Seq  int64 // lobby creation order
	Info[X]
}

// publish stores a fresh summary; called by the actor only (and by New
// before the actor starts).
func (r *Room[M, In, X]) publish() {
	r.summary.Store(&Summary[X]{Code: r.code, Seq: r.o.Seq, Info: r.game.Info()})
}

// Summary is the latest published summary; safe from any goroutine.
func (r *Room[M, In, X]) Summary() Summary[X] { return *r.summary.Load() }
