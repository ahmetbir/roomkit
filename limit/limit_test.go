package limit

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// clock is the injected time source: tests advance it by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time                { return c.t }
func (c *clock) add(d time.Duration) time.Time { c.t = c.t.Add(d); return c.t }

func newClock() *clock { return &clock{t: time.Unix(1_000_000, 0)} }

func TestBucketBurstThenRate(t *testing.T) {
	c := newClock()
	b := NewBucket(90, 30)
	for i := range 30 {
		if !b.Allow(c.now()) {
			t.Fatalf("burst token %d refused", i)
		}
	}
	if b.Allow(c.now()) {
		t.Fatal("31st message in the same instant allowed")
	}
	// 60 Hz input for 10 s stays well inside 90/s.
	for i := range 600 {
		if !b.Allow(c.add(time.Second / 60)) {
			t.Fatalf("60 Hz input refused at %d", i)
		}
	}
	// 200/s drains the bucket within a second.
	refused := false
	for range 200 {
		if !b.Allow(c.add(5 * time.Millisecond)) {
			refused = true
		}
	}
	if !refused {
		t.Fatal("200 msg/s never refused")
	}
}

func TestBucketEmptyDoesNotTake(t *testing.T) {
	c := newClock()
	b := NewBucket(2, 1)
	if b.Empty(c.now()) || b.Empty(c.now()) {
		t.Fatal("fresh bucket reported empty")
	}
	b.Allow(c.now())
	if !b.Empty(c.now()) {
		t.Fatal("drained bucket not empty")
	}
	if b.Empty(c.add(500 * time.Millisecond)) {
		t.Fatal("not refilled after 1/rate")
	}
}

func TestKeyedPerKeyAndRefill(t *testing.T) {
	c := newClock()
	k := NewKeyed(3, 3) // 3 per minute
	for range 3 {
		if !k.Allow("a", c.now()) {
			t.Fatal("burst refused")
		}
	}
	if k.Allow("a", c.now()) {
		t.Fatal("4th create in a minute allowed")
	}
	if !k.Allow("b", c.now()) {
		t.Fatal("other key throttled")
	}
	if !k.Blocked("a", c.now()) || k.Blocked("b", c.now()) || k.Blocked("new", c.now()) {
		t.Fatal("Blocked wrong")
	}
	if k.Allow("a", c.add(19*time.Second)) {
		t.Fatal("refilled too early")
	}
	if !k.Allow("a", c.add(2*time.Second)) {
		t.Fatal("not refilled after 20 s")
	}
}

func TestKeyedEvictsIdleKeys(t *testing.T) {
	c := newClock()
	k := NewKeyed(10, 10)
	for i := range 1000 {
		k.Allow(fmt.Sprint("ip", i), c.now())
	}
	if k.Len() != 1000 {
		t.Fatalf("len=%d", k.Len())
	}
	k.Allow("late", c.add(30*time.Second)) // half refilled: nothing to drop yet
	if k.Len() != 1001 {
		t.Fatalf("dropped partially used buckets: len=%d", k.Len())
	}
	k.Allow("later", c.add(2*time.Minute))
	if k.Len() != 1 {
		t.Fatalf("idle keys kept: len=%d", k.Len())
	}
}

func TestKeyedBounded(t *testing.T) {
	c := newClock()
	k := NewKeyed(1, 1)
	k.maxKeys = 4
	for i := range 4 {
		if !k.Allow(fmt.Sprint(i), c.now()) {
			t.Fatal("refused under cap")
		}
	}
	if !k.Full() || k.Allow("x", c.now()) || !k.Blocked("x", c.now()) {
		t.Fatal("table grew past maxKeys")
	}
	if k.Len() != 4 {
		t.Fatalf("len=%d", k.Len())
	}
	if !k.Allow("x", c.add(2*time.Minute)) || k.Full() {
		t.Fatal("no room after sweep")
	}
}

func TestGate(t *testing.T) {
	g := NewGate(3, 2)
	if g.Acquire("a") != nil || g.Acquire("a") != nil {
		t.Fatal("under caps refused")
	}
	if err := g.Acquire("a"); !errors.Is(err, ErrKey) {
		t.Fatalf("3rd per key: %v", err)
	}
	if g.Acquire("b") != nil {
		t.Fatal("b refused")
	}
	if err := g.Acquire("c"); !errors.Is(err, ErrTotal) {
		t.Fatalf("over total: %v", err)
	}
	g.Release("a")
	g.Release("zz") // unknown: no-op
	if g.Open() != 2 || g.Acquire("c") != nil {
		t.Fatalf("release: open=%d", g.Open())
	}
	g.Release("a")
	g.Release("b")
	g.Release("c")
	if g.Open() != 0 || len(g.per) != 0 {
		t.Fatalf("leak: open=%d keys=%d", g.Open(), len(g.per))
	}
}

// Take-then-refund keeps the check and the spend one atomic step: an
// undone action gives its token back, never past the burst.
func TestKeyedRefund(t *testing.T) {
	c := newClock()
	k := NewKeyed(1, 2)
	if !k.Allow("a", c.now()) || !k.Allow("a", c.now()) || k.Allow("a", c.now()) {
		t.Fatal("burst of 2 not enforced")
	}
	k.Refund("a", c.now())
	if !k.Allow("a", c.now()) || k.Allow("a", c.now()) {
		t.Fatal("refund must return exactly one token")
	}
	k.Refund("b", c.now()) // unknown key: no-op, no state
	k.Refund("a", c.now())
	k.Refund("a", c.now())
	k.Refund("a", c.now())
	if !k.Allow("a", c.now()) || !k.Allow("a", c.now()) || k.Allow("a", c.now()) {
		t.Fatal("refunds must not exceed the burst")
	}
	if k.Known("b") || !k.Known("a") {
		t.Fatal("Known")
	}
}
