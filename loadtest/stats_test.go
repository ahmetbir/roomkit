package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHistQuantiles(t *testing.T) {
	var h Hist
	for range 98 {
		h.Observe(33 * time.Millisecond)
	}
	h.Observe(70 * time.Millisecond)
	h.Observe(5 * time.Second) // overflow bucket
	c := h.Snapshot()
	if c.Total() != 100 {
		t.Fatalf("total = %d", c.Total())
	}
	if q := c.Quantile(0.5); q != 34 {
		t.Errorf("p50 = %d, want 34 (bucket [33,34))", q)
	}
	if q := c.Quantile(0.99); q != 71 {
		t.Errorf("p99 = %d, want 71", q)
	}
	if q := c.Quantile(1); q != HistMax+1 {
		t.Errorf("max = %d, want %d", q, HistMax+1)
	}
	if n := c.Above(50); n != 2 {
		t.Errorf("above 50 ms = %d, want 2", n)
	}
	var empty Counts
	if q := empty.Quantile(0.99); q != 0 {
		t.Errorf("empty p99 = %d, want 0", q)
	}
}

func TestHistSubIsTheWindow(t *testing.T) {
	var h Hist
	h.Observe(10 * time.Millisecond)
	old := h.Snapshot()
	h.Observe(40 * time.Millisecond)
	h.Observe(-time.Millisecond) // clamps to bucket 0
	d := h.Snapshot().Sub(old)
	if d.Total() != 2 || d.Quantile(1) != 41 || d[0] != 1 {
		t.Errorf("window = total %d max %d b0 %d, want 2, 41, 1", d.Total(), d.Quantile(1), d[0])
	}
}

func TestLineRatesPerSecond(t *testing.T) {
	st := NewStats()
	prev := st.Sample(0)
	st.Conns.Store(4)
	st.MsgsIn.Add(240)
	st.MsgsOut.Add(480)
	st.BytesIn.Add(4 * 2 * 120 * 1024) // 120 KB/s per client over 2 s
	st.Snaps.Add(240)
	st.Gaps.Add(3)
	st.SnapIv.Observe(33 * time.Millisecond)
	st.Disconnect("close:StatusPolicyViolation")
	cur := st.Sample(2 * time.Second)
	got := Line(prev, cur)
	for _, want := range []string{"conns=  4", "in=   120/s", "out=   240/s", "rx/client=120.0KB/s", "iv_p50=34ms", "gaps=3", "disc=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q lacks %q", got, want)
		}
	}
}

func TestSummaryListsReasonsSorted(t *testing.T) {
	st := NewStats()
	from := st.Sample(10 * time.Second)
	st.Conns.Store(2)
	st.BytesIn.Add(2 * 10 * 1024 * 100)
	to := st.Sample(20 * time.Second)
	got := Summary(from, to, Counts{}, map[string]int{"timeout": 1, "close:StatusGoingAway": 2})
	if !strings.Contains(got, "rx/client=100.0KB/s") {
		t.Errorf("summary lacks per-client rx:\n%s", got)
	}
	if !strings.HasSuffix(got, "disconnects: close:StatusGoingAway=2 timeout=1") {
		t.Errorf("summary reasons:\n%s", got)
	}
	if !strings.HasSuffix(Summary(from, to, Counts{}, nil), "disconnects: none") {
		t.Error("no reasons should read none")
	}
}

func TestReason(t *testing.T) {
	cases := []struct {
		err  error
		msg  string
		want string
	}{
		{nil, "oda dolu", "server:oda dolu"},
		{nil, "", "closed"},
		{websocket.CloseError{Code: websocket.StatusPolicyViolation}, "", "close:StatusPolicyViolation"},
		{fmt.Errorf("read: %w", context.DeadlineExceeded), "", "timeout"},
		{io.EOF, "", "eof"},
		{errors.New(strings.Repeat("x", 100)), "", "error:" + strings.Repeat("x", 60)},
	}
	for _, c := range cases {
		if got := Reason(c.err, c.msg); got != c.want {
			t.Errorf("reason(%v, %q) = %q, want %q", c.err, c.msg, got, c.want)
		}
	}
}
