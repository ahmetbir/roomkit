package server

import (
	"errors"
	"fmt"

	"github.com/ahmetbir/roomkit/limit"
)

// errNet is a refusal by the per-/48 aggregate cap; it is also limit.ErrKey
// (same answer to the client), told apart only in logs and metrics.
var errNet = fmt.Errorf("server: /48 full: %w", limit.ErrKey)

// isNet reports whether err is the /48 aggregate refusal.
func isNet(err error) bool { return errors.Is(err, errNet) }

// connGate caps open game sockets server-wide and per address key
// (limitKey: an IPv4 address or an IPv6 /64), and on top of that per IPv6
// /48 summed over its /64s, so one cheap /48 cannot fill the server.
type connGate struct {
	addr *limit.Gate // server-wide and per key
	net  *limit.Gate // per /48 only (its total never binds before addr's)
}

func newConnGate(l Limits) *connGate {
	return &connGate{addr: limit.NewGate(l.MaxConns, l.MaxConnsIP), net: limit.NewGate(l.MaxConns, l.MaxConnsNet)}
}

// Acquire takes a slot for key, or reports limit.ErrKey (the address is
// full; errNet, which is also ErrKey, when its /48 is) or limit.ErrTotal. Every nil return must be paired with one
// Release(key).
func (g *connGate) Acquire(key string) error {
	if err := g.addr.Acquire(key); err != nil {
		return err
	}
	if n, ok := netKey(key); ok {
		if err := g.net.Acquire(n); err != nil {
			g.addr.Release(key)
			return errNet
		}
	}
	return nil
}

func (g *connGate) Release(key string) {
	g.addr.Release(key)
	if n, ok := netKey(key); ok {
		g.net.Release(n)
	}
}
