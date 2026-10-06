// Package server wires HTTP routes and the WebSocket handshake to the lobby.
package server

import (
	"io/fs"
	"math"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/ahmetbir/roomkit/limit"
	"github.com/ahmetbir/roomkit/lobby"
	"github.com/ahmetbir/roomkit/metrics"
	"github.com/ahmetbir/roomkit/room"
	"github.com/ahmetbir/roomkit/wsconn"
)

type Options struct {
	Web              fs.FS          // built client; index.html at the root
	Lag              time.Duration  // artificial outbound latency
	Origins          []string       // allowed WebSocket origin hosts; empty = same host only
	HandshakeTimeout time.Duration  // hello + create/join must finish within it; default 5s
	TrustProxy       []netip.Prefix // peers whose X-Real-IP names the client
	ConnectSrc       []string       // extra CSP connect-src sources, e.g. wss://host
	Limits           Limits
	Now              func() time.Time  // clock for the limiters; default time.Now
	Stats            Stats             // nil or not ready: /api/leaderboard and /api/me answer 503
	Metrics          *metrics.Registry // nil = not measured
}

// Limits bounds what one address, one connection and the whole server may
// use. Zero (or negative) fields take the defaults in DefaultLimits.
type Limits struct {
	MaxConns         int     // open game sockets, server-wide
	MaxConnsIP       int     // open game sockets per client address
	MaxConnsNet      int     // open game sockets per IPv6 /48, summed over its /64 addresses
	CreatePerMinIP   float64 // room creations per address per minute (burst = same)
	JoinFailPerMinIP float64 // failed joins per address per minute (burst = same)
	JoinPerMinIP     float64 // successful joins per address per minute (burst = same)
	MsgRate          float64 // inbound messages per second per connection
	MsgBurst         int
	PickRate         float64 // pick messages per second per connection
	PickBurst        int
	PingRate         float64 // ping messages per second per connection
	PingBurst        int
	APIPerMinIP      float64 // JSON API requests per address per minute
	APIBurst         int
}

func DefaultLimits() Limits {
	return Limits{
		MaxConns: 128, MaxConnsIP: 6, MaxConnsNet: 24, CreatePerMinIP: 3, JoinFailPerMinIP: 10, JoinPerMinIP: 20,
		MsgRate: 90, MsgBurst: 120, PickRate: 2, PickBurst: 4, PingRate: 2, PingBurst: 4,
		APIPerMinIP: 120, APIBurst: 30,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	set := func(v *float64, def float64) {
		if *v <= 0 {
			*v = def
		}
	}
	seti := func(v *int, def int) {
		if *v <= 0 {
			*v = def
		}
	}
	seti(&l.MaxConns, d.MaxConns)
	seti(&l.MaxConnsIP, d.MaxConnsIP)
	seti(&l.MaxConnsNet, d.MaxConnsNet)
	set(&l.CreatePerMinIP, d.CreatePerMinIP)
	set(&l.JoinFailPerMinIP, d.JoinFailPerMinIP)
	set(&l.JoinPerMinIP, d.JoinPerMinIP)
	set(&l.MsgRate, d.MsgRate)
	seti(&l.MsgBurst, d.MsgBurst)
	set(&l.PickRate, d.PickRate)
	seti(&l.PickBurst, d.PickBurst)
	set(&l.PingRate, d.PingRate)
	seti(&l.PingBurst, d.PingBurst)
	set(&l.APIPerMinIP, d.APIPerMinIP)
	seti(&l.APIBurst, d.APIBurst)
	return l
}

// Server is the HTTP handler: the client at / and /r/{code}, static files
// under /, /healthz, the JSON API under /api/ (rooms, leaderboard, me), and
// the game socket at /ws. S, M, In and X are the game's settings, client
// message, tick input and lobby summary types; k is the game's Kit.
type Server[S any, M Msg[M, In], In room.Input[In], X any] struct {
	lobby     *lobby.Lobby[S, M, In, X]
	kit       Kit[S, M, X]
	o         Options
	h         http.Handler
	page      []byte // index.html with content-hashed bundle URLs; nil if not built
	conns     *connGate
	creates   *limit.Keyed
	joinFails *limit.Keyed
	joins     *limit.Keyed
	apis      *limit.Keyed
	rejects   *rejectLog
	rooms     apiCache              // GET /api/rooms body
	boards    map[BoardID]*apiCache // GET /api/leaderboard body per allowed board (Stats.Boards)
	// quickPick picks a room for quick play; the lobby's Quick, swapped in
	// tests to race the picked room.
	quickPick func() (*room.Room[M, In, X], bool)
	sockets   sync.WaitGroup
	drain     drain
}

func New[S any, M Msg[M, In], In room.Input[In], X any](l *lobby.Lobby[S, M, In, X], k Kit[S, M, X], o Options) *Server[S, M, In, X] {
	if o.Web == nil {
		o.Web = emptyFS{}
	}
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = 5 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	o.Limits = o.Limits.withDefaults()
	lim := o.Limits
	s := &Server[S, M, In, X]{
		lobby: l, kit: k, o: o, page: loadPage(o.Web),
		conns:     newConnGate(lim),
		creates:   limit.NewKeyed(lim.CreatePerMinIP, burstOf(lim.CreatePerMinIP)),
		joinFails: limit.NewKeyed(lim.JoinFailPerMinIP, burstOf(lim.JoinFailPerMinIP)),
		joins:     limit.NewKeyed(lim.JoinPerMinIP, burstOf(lim.JoinPerMinIP)),
		apis:      limit.NewKeyed(lim.APIPerMinIP, lim.APIBurst),
		rejects:   newRejectLog(o.Now, rejectCounter(o.Metrics)),
		boards:    boards(o.Stats),
		quickPick: l.Quick,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /r/{code}", s.index)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /api/rooms", s.apiRooms)
	mux.HandleFunc("GET /api/leaderboard", s.apiLeaderboard)
	mux.HandleFunc("GET /api/me", s.apiMe)
	mux.HandleFunc("GET /api/", s.apiNotFound)
	mux.Handle("GET /", static(o.Web))
	mux.HandleFunc("GET /ws", s.counted(s.socket))
	s.h = secure(mux, csp(o.ConnectSrc))
	return s
}

// boards is one leaderboard cache per board the stats allow (none when
// stats are off): the ?period=&key= whitelist.
func boards(st Stats) map[BoardID]*apiCache {
	b := map[BoardID]*apiCache{}
	if st != nil {
		for _, id := range st.Boards() {
			b[id] = &apiCache{}
		}
	}
	return b
}

// burstOf is the burst for a per-minute rate: the rate rounded up, at least
// 1, so a fractional rate never blocks everything.
func burstOf(perMinute float64) int { return max(1, int(math.Ceil(perMinute))) }

func (s *Server[S, M, In, X]) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.h.ServeHTTP(w, r) }

// Wait blocks until every game socket handler has finished, close
// handshakes included. Call it after http.Server.Shutdown, which stops new
// sockets; rooms end when the lobby's context is cancelled.
func (s *Server[S, M, In, X]) Wait() { s.sockets.Wait() }

// rejectCounter counts every refusal by reason; nil without metrics.
func rejectCounter(m *metrics.Registry) func(string) {
	if m == nil {
		return nil
	}
	return m.Rejects.Inc
}

// connCounters feeds a connection's writes and snapshot drops to the registry.
type connCounters struct{ r *metrics.Registry }

func (c connCounters) Out()  { c.r.MsgsOut.Inc() }
func (c connCounters) Drop() { c.r.SnapDrops.Inc() }

// counters is the wsconn hook; nil (not a nil-holding value) without metrics.
func (s *Server[S, M, In, X]) counters() wsconn.Counters {
	if s.o.Metrics == nil {
		return nil
	}
	return connCounters{s.o.Metrics}
}

// connsGauge moves the open game sockets gauge.
func (s *Server[S, M, In, X]) connsGauge(d int64) {
	if s.o.Metrics != nil {
		s.o.Metrics.Conns.Add(d)
	}
}
