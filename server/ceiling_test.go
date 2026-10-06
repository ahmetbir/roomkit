package server

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ahmetbir/roomkit/internal/fakegame"
	"github.com/ahmetbir/roomkit/netproto"
)

// Inputs are only dropped, but sustained spam (here 400/s) still ends the
// connection at the hard ceiling, within one 5 s window.
func TestSustainedInputSpamKicked(t *testing.T) {
	srv := newServer(t, Options{Web: web, Limits: tight(func(*Limits) {})})
	c, _ := joined(t, srv.URL)
	start := time.Now()
	closed := make(chan websocket.StatusCode, 1)
	go func() { closed <- c.closeStatus(8 * time.Second) }()
	tk := time.NewTicker(time.Second / 400)
	defer tk.Stop()
	for seq := 1; ; seq++ {
		select {
		case got := <-closed:
			if got != websocket.StatusPolicyViolation {
				t.Fatalf("close status %v, want policy violation", got)
			}
			if d := time.Since(start); d > 6*time.Second {
				t.Fatalf("kicked after %v, want within ~5-6 s", d)
			}
			return
		case <-tk.C:
			if seq <= 400*7 {
				c.writeRaw(inMsg(seq))
			}
		}
	}
}

// One 5 s stall (300 inputs at once) and live input after it: no kick.
func TestFiveSecondStallBurstNotKicked(t *testing.T) {
	srv := newServer(t, Options{Web: web, Limits: tight(func(*Limits) {})})
	c, _ := joined(t, srv.URL)
	seq := 0
	for range 300 {
		seq++
		c.writeRaw(inMsg(seq))
	}
	tk := time.NewTicker(time.Second / 60)
	defer tk.Stop()
	for range 60 {
		<-tk.C
		seq++
		if !c.writeRaw(inMsg(seq)) {
			t.Fatalf("closed at seq %d", seq)
		}
	}
	want := `"ack":` + strconv.Itoa(seq)
	c.until("snap", 2*time.Second, func(b []byte) bool { return strings.Contains(string(b), want) })
}

func TestCeilingBytes(t *testing.T) {
	now := time.Unix(100, 0)
	var c ceiling
	for i := range 160 { // 2 KB frames at 40/s: 80 KB/s
		if lim := c.add(now, 2048); lim != "" {
			t.Fatalf("frame %d: %s before 320 KB", i, lim)
		}
		now = now.Add(time.Second / 40)
	}
	if lim := c.add(now.Add(-time.Second/40), 2048); lim != "ceiling-bytes" {
		t.Fatalf("over 64 KB/s for 5 s = %q", lim)
	}
	if lim := c.add(now.Add(time.Second), 2048); lim != "" {
		t.Fatalf("a new window starts clean, got %q", lim)
	}
}

// Presses inside inputs dropped over the rate reach the room with the next
// admitted input, as the room's own backlog trim keeps them.
func TestDroppedInputPressesLatch(t *testing.T) {
	now := time.Unix(100, 0)
	p := &peer[fakegame.Msg]{ip: "x", guard: newMsgGuard(Limits{MsgRate: 1, MsgBurst: 1, PickRate: 1, PickBurst: 1, PingRate: 1, PingBurst: 1},
		func() time.Time { return now })}
	s := &fakeServer{kit: fakeKit{}}
	feed := func(raw string) (fakegame.Msg, bool) {
		t.Helper()
		m, ok, err := s.admit(p, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return m, ok
	}
	if _, ok := feed(`{"t":"in","seq":1}`); !ok {
		t.Fatal("first input dropped")
	}
	if _, ok := feed(`{"t":"in","seq":2,"shot":true}`); ok {
		t.Fatal("input over the burst admitted")
	}
	feed(`{"t":"in","seq":3}`)
	feed(`{"t":"in","seq":4}`)
	now = now.Add(time.Second)
	m, ok := feed(`{"t":"in","seq":5}`)
	if !ok || !m.Shot || m.Seq != 5 {
		t.Fatalf("next admitted input = %+v ok=%v, want seq 5 with the dropped shot", m, ok)
	}
	now = now.Add(time.Second)
	if m, _ := feed(`{"t":"in","seq":6}`); m.Shot {
		t.Fatal("the latch is cleared once used")
	}
	if p.drops.total != 3 {
		t.Fatalf("drops = %d", p.drops.total)
	}
}

// The client reconnects on exactly the flood kick's code (RECOVERABLE in
// ts/net/codes.ts); the two must not drift.
func TestFloodTextMatchesClient(t *testing.T) {
	b, err := os.ReadFile("../ts/net/codes.ts")
	if err != nil {
		t.Fatal(err)
	}
	if errCode(msgFlood) != netproto.CodeFlood {
		t.Fatalf("errCode(msgFlood) = %q", errCode(msgFlood))
	}
	if want := `RECOVERABLE = new Set(["` + netproto.CodeFlood + `"])`; !strings.Contains(string(b), want) {
		t.Fatalf("ts/net/codes.ts lacks %s", want)
	}
}
