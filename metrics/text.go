package metrics

import (
	"bytes"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
)

// WriteText writes the Prometheus text format (version 0.0.4).
func (r *Registry) WriteText(w io.Writer) error {
	var b bytes.Buffer
	gauge(&b, r.ns+"_rooms", "Open rooms.", r.Rooms.Load())
	gauge(&b, r.ns+"_humans", "Human players in rooms.", r.Humans.Load())
	gauge(&b, r.ns+"_bots", "Bots in rooms.", r.Bots.Load())
	gauge(&b, r.ns+"_conns", "Open WebSocket connections.", r.Conns.Load())
	histogram(&b, r.ns+"_tick_seconds", "Room tick duration in seconds.", r.TickSeconds)
	counter(&b, r.ns+"_tick_overruns_total", "Ticks longer than 16.7 ms.", r.TickOverruns.Load())
	counter(&b, r.ns+"_msgs_in_total", "Client messages received.", r.MsgsIn.Load())
	counter(&b, r.ns+"_msgs_out_total", "Server messages sent.", r.MsgsOut.Load())
	counter(&b, r.ns+"_snap_drops_total", "Snapshots dropped on full send queues.", r.SnapDrops.Load())
	var sd uint64
	if r.StatsDropped != nil {
		sd = r.StatsDropped()
	}
	counter(&b, r.ns+"_stats_dropped_total", "Pilot stat deltas dropped.", sd)
	vec(&b, r.ns+"_rejects_total", "Rejected connections and requests by reason.", r.Rejects)
	vec(&b, r.ns+"_api_requests_total", "API requests by path.", r.API)
	gauge(&b, "go_goroutines", "Number of goroutines.", int64(runtime.NumGoroutine()))
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms) // once per scrape
	gauge(&b, "go_memstats_heap_alloc_bytes", "Heap bytes allocated and in use.", int64(ms.HeapAlloc))
	_, err := w.Write(b.Bytes())
	return err
}

func head(b *bytes.Buffer, name, help, typ string) {
	b.WriteString("# HELP " + name + " " + help + "\n# TYPE " + name + " " + typ + "\n")
}

func gauge(b *bytes.Buffer, name, help string, v int64) {
	head(b, name, help, "gauge")
	b.WriteString(name + " " + strconv.FormatInt(v, 10) + "\n")
}

func counter(b *bytes.Buffer, name, help string, v uint64) {
	head(b, name, help, "counter")
	b.WriteString(name + " " + strconv.FormatUint(v, 10) + "\n")
}

func vec(b *bytes.Buffer, name, help string, v *CounterVec) {
	head(b, name, help, "counter")
	for _, val := range v.values {
		b.WriteString(name + "{" + v.label + "=\"" + escapeLabel(val) + "\"} " + strconv.FormatUint(v.byVal[val].Load(), 10) + "\n")
	}
}

func histogram(b *bytes.Buffer, name, help string, h *Histogram) {
	head(b, name, help, "histogram")
	cum := h.snapshot()
	for i, le := range h.bounds {
		b.WriteString(name + "_bucket{le=\"" + float(le) + "\"} " + strconv.FormatUint(cum[i], 10) + "\n")
	}
	total := strconv.FormatUint(cum[len(cum)-1], 10)
	b.WriteString(name + "_bucket{le=\"+Inf\"} " + total + "\n")
	b.WriteString(name + "_sum " + float(h.sumValue()) + "\n")
	b.WriteString(name + "_count " + total + "\n")
}

// escapeLabel escapes a label value per the text format: \ " and newline.
func escapeLabel(s string) string {
	if !strings.ContainsAny(s, "\\\"\n") {
		return s
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func float(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// Handler serves GET/HEAD /metrics only: other paths 404, other methods 405.
func Handler(r *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/metrics" {
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = r.WriteText(w) // headers are sent; a client gone mid-body has nothing to hear back
	})
}
