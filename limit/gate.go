package limit

import (
	"errors"
	"sync"
)

var (
	ErrTotal = errors.New("limit: too many connections")
	ErrKey   = errors.New("limit: too many connections from one address")
)

// Gate caps concurrent holders in total and per key. Keys with no holder
// are deleted, so memory follows open connections. Safe for concurrent use.
type Gate struct {
	total, perKey int

	mu  sync.Mutex
	n   int
	per map[string]int
}

func NewGate(total, perKey int) *Gate {
	return &Gate{total: total, perKey: perKey, per: map[string]int{}}
}

// Acquire takes a slot for key or reports which cap is reached. Every nil
// return must be paired with one Release(key).
func (g *Gate) Acquire(key string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.per[key] >= g.perKey {
		return ErrKey
	}
	if g.n >= g.total {
		return ErrTotal
	}
	g.n++
	g.per[key]++
	return nil
}

func (g *Gate) Release(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.per[key] == 0 {
		return
	}
	g.n--
	if g.per[key]--; g.per[key] == 0 {
		delete(g.per, key)
	}
}

// Open is the number of slots held.
func (g *Gate) Open() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}
