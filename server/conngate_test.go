package server

import (
	"errors"
	"testing"

	"github.com/ahmetbir/roomkit/limit"
)

func TestConnGateReleasesBothCaps(t *testing.T) {
	g := newConnGate(Limits{MaxConns: 100, MaxConnsIP: 2, MaxConnsNet: 2})
	a, b := "2001:db8:1:1::/64", "2001:db8:1:2::/64"
	if g.Acquire(a) != nil || g.Acquire(b) != nil {
		t.Fatal("under the caps")
	}
	if err := g.Acquire(a); !errors.Is(err, limit.ErrKey) || !isNet(err) {
		t.Fatalf("/48 full = %v, want ErrKey and errNet", err)
	}
	g2 := newConnGate(Limits{MaxConns: 100, MaxConnsIP: 1, MaxConnsNet: 9})
	if g2.Acquire(a) != nil {
		t.Fatal("first")
	}
	if err := g2.Acquire(a); !errors.Is(err, limit.ErrKey) || isNet(err) {
		t.Fatalf("/64 full = %v, want ErrKey, not errNet", err)
	}
	// The refused /48 slot must not leak a /64 slot: a has one left.
	g.Release(b)
	if err := g.Acquire(a); err != nil {
		t.Fatalf("after a release = %v", err)
	}
	if g.addr.Open() != 2 || g.net.Open() != 2 {
		t.Fatalf("open addr %d net %d, want 2 2", g.addr.Open(), g.net.Open())
	}
	g.Release(a)
	g.Release(a)
	if g.addr.Open() != 0 || g.net.Open() != 0 {
		t.Fatalf("open addr %d net %d after releases", g.addr.Open(), g.net.Open())
	}
	if g.Acquire("198.51.100.1") != nil || g.net.Open() != 0 {
		t.Fatal("IPv4 takes an address slot and no aggregate slot")
	}
	g.Release("198.51.100.1")
	if g.addr.Open() != 0 {
		t.Fatal("IPv4 slot not released")
	}
}
