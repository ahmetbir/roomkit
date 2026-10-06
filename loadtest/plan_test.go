package loadtest

import (
	"testing"
	"time"
)

func TestAssignRoundRobinWithOneCreatorPerRoom(t *testing.T) {
	got := Assign(7, 3)
	want := []Slot{{0, true}, {1, true}, {2, true}, {0, false}, {1, false}, {2, false}, {0, false}}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slot %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if n := HumansPerRoom(7, 3); n != 3 {
		t.Errorf("HumansPerRoom(7,3) = %d, want 3", n)
	}
}

func TestAssignQuickPlay(t *testing.T) {
	for i, s := range Assign(4, 0) {
		if s.Room != -1 || s.Creator {
			t.Errorf("slot %d = %+v, want quick play", i, s)
		}
	}
	if n := HumansPerRoom(4, 0); n != 0 {
		t.Errorf("HumansPerRoom(4,0) = %d, want 0", n)
	}
}

func TestStartAtSpreadsOverRamp(t *testing.T) {
	ramp := 10 * time.Second
	if d := StartAt(0, 4, ramp); d != 0 {
		t.Errorf("first = %v, want 0", d)
	}
	if d := StartAt(2, 4, ramp); d != 5*time.Second {
		t.Errorf("middle = %v, want 5s", d)
	}
	if d := StartAt(3, 4, 0); d != 0 {
		t.Errorf("no ramp = %v, want 0", d)
	}
	for i := range 4 {
		if d := StartAt(i, 4, ramp); d >= ramp {
			t.Errorf("player %d starts at %v, not before the ramp ends", i, d)
		}
	}
}

func TestWSURL(t *testing.T) {
	ok := map[string]string{
		"ws://127.0.0.1:8080":    "ws://127.0.0.1:8080/ws",
		"ws://dogfight-lt:8080/": "ws://dogfight-lt:8080/ws",
		"wss://example.test/ws":  "wss://example.test/ws",
		"ws://h:1/other?x=1":     "ws://h:1/other?x=1",
	}
	for in, want := range ok {
		got, err := WSURL(in)
		if err != nil || got != want {
			t.Errorf("WSURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://h:8080", "ws://", "h:8080", "::"} {
		if _, err := WSURL(bad); err == nil {
			t.Errorf("WSURL(%q) accepted", bad)
		}
	}
}
