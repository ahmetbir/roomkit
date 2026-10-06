package server

import (
	"testing"
	"time"

	"github.com/ahmetbir/roomkit/netproto"
)

func TestGuardVerdicts(t *testing.T) {
	now := time.Unix(0, 0)
	g := newMsgGuard(Limits{MsgRate: 90, MsgBurst: 3, PickRate: 2, PickBurst: 1, PingRate: 2, PingBurst: 1}, func() time.Time { return now })
	for i := range 3 {
		if v, _ := g.check(netproto.TIn, ClassAll); v != pass {
			t.Fatalf("in %d: %v", i, v)
		}
	}
	if v, b := g.check(netproto.TIn, ClassAll); v != drop || b != "in" {
		t.Fatalf("in over burst = %v %q, want drop in", v, b)
	}
	if v, _ := g.check("pick", ClassChoice); v != pass {
		t.Fatal("first pick refused: inputs share no bucket with other kinds")
	}
	if v, b := g.check("pick", ClassChoice); v != kick || b != "pick" {
		t.Fatalf("pick over burst = %v %q", v, b)
	}
	if v, _ := g.check(netproto.TPing, ClassAll); v != pass {
		t.Fatal("first ping refused")
	}
	if v, b := g.check(netproto.TPing, ClassAll); v != kick || b != "ping" {
		t.Fatalf("ping over burst = %v %q", v, b)
	}
	g.check(netproto.TChat, ClassAll)
	if v, b := g.check(netproto.TChat, ClassAll); v != kick || b != "all" {
		t.Fatalf("chat over the shared burst = %v %q", v, b)
	}
	now = now.Add(time.Second)
	if v, _ := g.check(netproto.TIn, ClassAll); v != pass {
		t.Fatal("inputs pass again after a refill")
	}
}

// A network stall delivers queued 60 Hz inputs in one bunch: up to the burst
// (2 s worth) must pass the guard at one instant, and only then are they dropped.
func TestBunchedInputPassesTheGuard(t *testing.T) {
	now := time.Unix(0, 0)
	l := Limits{}.withDefaults()
	g := newMsgGuard(l, func() time.Time { return now })
	for i := range 120 {
		if v, b := g.check(netproto.TIn, ClassAll); v != pass {
			t.Fatalf("input %d of a 120 bunch: %v %q", i+1, v, b)
		}
	}
	if v, b := g.check(netproto.TIn, ClassAll); v != drop || b != "in" {
		t.Fatalf("input 121 = %v %q, want drop in", v, b)
	}
}

// Every choice-class type draws on one choice bucket (Dogfight: a team
// choice opens the pick screen, so team and pick share it).
func TestGuardChoiceTypesShareOneBucket(t *testing.T) {
	now := time.Unix(0, 0)
	g := newMsgGuard(Limits{MsgRate: 90, MsgBurst: 10, PickRate: 2, PickBurst: 2, PingRate: 2, PingBurst: 1}, func() time.Time { return now })
	if v, _ := g.check("team", ClassChoice); v != pass {
		t.Fatal("first team refused")
	}
	if v, _ := g.check("pick", ClassChoice); v != pass {
		t.Fatal("pick after one team refused")
	}
	if v, b := g.check("team", ClassChoice); v != kick || b != "pick" {
		t.Fatalf("team over the pick burst = %v %q", v, b)
	}
}
