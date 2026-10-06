package ledger

import "testing"

func TestSlotQueuesUntilSetThenForwards(t *testing.T) {
	c := &clock{t0}
	sl := NewSlot[delta, record]()
	if sl.Ready() {
		t.Fatal("ready before a store is set")
	}
	if !sl.Record(delta{"aa", 1}) {
		t.Fatal("a waiting slot must queue")
	}
	if _, _, ok := sl.Get("aa"); ok {
		t.Fatal("Get answered before a store is set")
	}
	st := open(t, t.TempDir(), c, Options{})
	if !sl.Set(st) {
		t.Fatal("Set on a waiting slot")
	}
	if !sl.Ready() {
		t.Fatal("not ready after Set")
	}
	sl.Record(delta{"aa", 2})
	if r, _, ok := sl.Get("aa"); !ok || r.Wins != 3 {
		t.Fatalf("queued + forwarded wins: %+v %v", r, ok)
	}
	n := 0
	sl.View(func(all map[string]Entry[record]) { n = len(all) })
	if n != 1 {
		t.Fatalf("view saw %d entries", n)
	}
	if err := sl.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSlotQueueIsBounded(t *testing.T) {
	sl := NewSlot[delta, record]()
	for range slotQueue {
		sl.Record(delta{"aa", 1})
	}
	if sl.Record(delta{"aa", 1}) {
		t.Fatal("record past the queue bound was accepted")
	}
	if sl.Dropped() != 1 {
		t.Fatalf("dropped %d, want 1", sl.Dropped())
	}
}

// Close releases the store (and its lock); a store that opens after Close
// is closed at once; Reopen starts waiting again.
func TestSlotCloseAndReopen(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	sl := NewSlot[delta, record]()
	sl.Set(open(t, dir, c, Options{}))
	if err := sl.Close(); err != nil {
		t.Fatal(err)
	}
	if sl.Ready() || sl.Record(delta{"aa", 1}) {
		t.Fatal("closed slot is ready or takes records")
	}
	late := open(t, dir, c, Options{}) // the lock was released by Close
	if sl.Set(late) {
		t.Fatal("Set on a closed slot must refuse (and close the store)")
	}
	again := open(t, dir, c, Options{}) // late's lock is gone too
	again.Close()

	sl.Reopen()
	if !sl.Record(delta{"bb", 4}) {
		t.Fatal("reopened slot must queue")
	}
	sl.Set(open(t, dir, c, Options{}))
	defer sl.Close()
	if r, _, ok := sl.Get("bb"); !ok || r.Wins != 4 {
		t.Fatalf("after reopen: %+v %v", r, ok)
	}
}
