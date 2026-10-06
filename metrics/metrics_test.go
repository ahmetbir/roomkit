package metrics

import (
	"bytes"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExposition(t *testing.T) {
	r := New("dogfight", func() uint64 { return 7 })
	r.Rooms.Set(2)
	r.Humans.Add(3)
	r.MsgsIn.Add(10)
	r.Rejects.Inc("flood")
	r.Rejects.Inc("made-up")
	r.TickSeconds.Observe(0.003)
	r.TickSeconds.Observe(0.02)
	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# TYPE dogfight_rooms gauge\ndogfight_rooms 2\n",
		"dogfight_humans 3\n",
		"dogfight_msgs_in_total 10\n",
		`dogfight_rejects_total{reason="flood"} 1`,
		`dogfight_rejects_total{reason="other"} 1`,
		`dogfight_tick_seconds_bucket{le="0.004"} 1`,
		`dogfight_tick_seconds_bucket{le="0.025"} 2`,
		`dogfight_tick_seconds_bucket{le="+Inf"} 2`,
		"dogfight_tick_seconds_count 2\n",
		"dogfight_stats_dropped_total 7\n",
		"go_goroutines ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "made-up") {
		t.Fatal("unknown label values must fold into other")
	}
}

func TestHandlerAndSummary(t *testing.T) {
	r := New("dogfight", nil)
	h := Handler(r)
	for path, code := range map[string]int{"/metrics": 200, "/": 404, "/debug/pprof/": 404} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != code {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
	if s := r.Summary(); !strings.HasPrefix(s, "rooms=0 humans=0 bots=0 conns=0 tick_p99_ms=") {
		t.Fatalf("summary %q", s)
	}
}

func TestHistogramQuantileAndSum(t *testing.T) {
	h := NewHistogram([]float64{0.001, 0.002, 0.004})
	if h.Quantile(0.99) != 0 {
		t.Fatal("empty histogram quantile is 0")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				h.Observe(0.0015)
			}
		}()
	}
	wg.Wait()
	h.Observe(1)
	if q := h.Quantile(0.5); q != 0.002 {
		t.Fatalf("p50 %v", q)
	}
	if q := h.Quantile(1); !math.IsInf(q, 1) {
		t.Fatalf("p100 lands in +Inf, got %v", q)
	}
	if s := h.sumValue(); math.Abs(s-(800*0.0015+1)) > 1e-9 {
		t.Fatalf("sum %v", s)
	}
}

func TestSummaryFormat(t *testing.T) {
	r := New("dogfight", nil)
	r.Rooms.Set(1)
	r.Humans.Set(2)
	r.Bots.Set(6)
	r.Conns.Set(2)
	r.TickSeconds.Observe(0.0019)
	r.TickOverruns.Inc()
	r.SnapDrops.Add(3)
	if s := r.Summary(); s != "rooms=1 humans=2 bots=6 conns=2 tick_p99_ms=2.0 overruns=1 drops=3" {
		t.Fatalf("summary %q", s)
	}
}

func TestHandlerHeadersAndHead(t *testing.T) {
	r := New("dogfight", nil)
	r.API.Inc("me")
	h := Handler(r)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `dogfight_api_requests_total{path="me"} 1`) ||
		!strings.Contains(body, "dogfight_stats_dropped_total 0\n") ||
		!strings.Contains(body, "go_memstats_heap_alloc_bytes ") {
		t.Fatalf("body\n%s", body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD: %d", rec.Code)
	}
}

func TestHistogramIgnoresNonFinite(t *testing.T) {
	h := NewHistogram([]float64{0.004, 0.001, 0.002, 0.002, math.NaN()})
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0.0015} {
		h.Observe(v)
	}
	if h.Invalid() != 3 || h.Count() != 1 || h.sumValue() != 0.0015 || h.Quantile(1) != 0.002 {
		t.Fatalf("invalid %d sum %v p100 %v", h.Invalid(), h.sumValue(), h.Quantile(1))
	}
	var b strings.Builder
	r := New("dogfight", nil)
	r.TickSeconds.Observe(math.NaN())
	r.WriteText(&b)
	if !strings.Contains(b.String(), "dogfight_tick_seconds_sum 0\n") || !strings.Contains(b.String(), "dogfight_tick_seconds_count 0\n") {
		t.Fatalf("NaN reached the exposition:\n%s", b.String())
	}
	if len(h.bounds) != 3 || h.bounds[0] != 0.001 || h.bounds[2] != 0.004 {
		t.Fatalf("bounds not sorted/deduplicated: %v", h.bounds)
	}
}

func TestLabelValuesEscaped(t *testing.T) {
	v := NewCounterVec("path", `a"b`, `c\d`, "e\nf")
	v.Inc(`a"b`)
	v.Inc(`c\d`)
	v.Inc("e\nf")
	var b bytes.Buffer
	vec(&b, "x_total", "help", v)
	for _, want := range []string{`x_total{path="a\"b"} 1`, `x_total{path="c\\d"} 1`, `x_total{path="e\nf"} 1`} {
		if !strings.Contains(b.String(), want+"\n") {
			t.Fatalf("missing %s in\n%s", want, b.String())
		}
	}
}

func TestNamespacePrefixesEveryName(t *testing.T) {
	var b bytes.Buffer
	if err := New("f1", nil).WriteText(&b); err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		name := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(l, "# HELP "), "# TYPE "))[0]
		if !strings.HasPrefix(name, "f1_") && !strings.HasPrefix(name, "go_") {
			t.Fatalf("unprefixed metric %q", l)
		}
	}
}
