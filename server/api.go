package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ahmetbir/roomkit/pilot"
)

const (
	roomsTTL = time.Second      // how long one /api/rooms body is served before it is rebuilt
	boardTTL = 10 * time.Second // same for each /api/leaderboard period
)

// User-facing API error texts.
const (
	msgStatsOff  = "istatistik kapalı"
	msgBadPeriod = "geçersiz dönem"
	msgNoAPI     = "bulunamadı"
	msgRequests  = "çok fazla istek"
)

// apiCache holds one JSON body for ttl; a nil body from build is not kept.
type apiCache struct {
	mu   sync.Mutex
	at   time.Time
	body []byte
}

func (c *apiCache) get(now time.Time, ttl time.Duration, build func() []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body == nil || now.Sub(c.at) >= ttl {
		c.body, c.at = build(), now
	}
	return c.body
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// errorJSON is an API error body: the Turkish text and its stable code.
func errorJSON(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg, "code": errCode(msg)})
	return b
}

// apiAllow spends one API token of the caller's address; when none is left
// it answers 429 (503 while draining) and reports false.
func (s *Server[S, M, In, X]) apiAllow(w http.ResponseWriter, r *http.Request) bool {
	if s.o.Metrics != nil {
		s.o.Metrics.API.Inc(strings.TrimPrefix(r.URL.Path, "/api/")) // fixed set; anything else is "other"
	}
	if s.apiDraining(w) {
		return false
	}
	ip := clientIP(r, s.o.TrustProxy)
	key := limitKey(ip)
	if s.apis.Allow(key, s.o.Now()) {
		return true
	}
	s.rejects.note(s.keyedReason(s.apis, key, "api-rate"), ip.String())
	writeJSON(w, http.StatusTooManyRequests, errorJSON(msgRequests))
	return false
}

// apiRooms is GET /api/rooms: the listed rooms, rebuilt at most once per roomsTTL.
func (s *Server[S, M, In, X]) apiRooms(w http.ResponseWriter, r *http.Request) {
	if !s.apiAllow(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.rooms.get(s.o.Now(), roomsTTL, func() []byte {
		list := s.lobby.List()
		out := struct {
			Rooms []any `json:"rooms"`
		}{Rooms: make([]any, 0, len(list))}
		for _, x := range list {
			out.Rooms = append(out.Rooms, s.kit.Row(x))
		}
		b, _ := json.Marshal(out)
		return b
	}))
}

// apiLeaderboard is GET /api/leaderboard?period=week|all: the top pilots,
// rebuilt at most once per boardTTL per period.
func (s *Server[S, M, In, X]) apiLeaderboard(w http.ResponseWriter, r *http.Request) {
	if !s.apiAllow(w, r) {
		return
	}
	if s.o.Stats == nil || !s.o.Stats.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, errorJSON(msgStatsOff))
		return
	}
	name := r.URL.Query().Get("period")
	c, ok := s.boards[name] // whitelisted (Stats.Periods) before it is echoed
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorJSON(msgBadPeriod))
		return
	}
	body := c.get(s.o.Now(), boardTTL, func() []byte { return s.o.Stats.Board(name) })
	if body == nil { // the store closed after Ready: no board, nothing cached
		writeJSON(w, http.StatusServiceUnavailable, errorJSON(msgStatsOff))
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// noPilot is /api/me's answer for a missing, malformed, oversize or unknown
// token: one body for all, so it never tells which.
var noPilot = []byte(`{"pilot":null}`)

// dummyToken is hashed in place of a malformed token, so every answer
// costs one hash and one lookup (no timing tell between bad and unknown).
const dummyToken = "AAAAAAAAAAAAAAAAAAAAAA"

// apiMe is GET /api/me with the X-Pilot-Token header: {"pilot":{...}} with
// the caller's own card, or {"pilot":null}. Stats off: 503.
func (s *Server[S, M, In, X]) apiMe(w http.ResponseWriter, r *http.Request) {
	if !s.apiAllow(w, r) {
		return
	}
	if s.o.Stats == nil || !s.o.Stats.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, errorJSON(msgStatsOff))
		return
	}
	tok := r.Header.Get(pilot.Header)
	valid := pilot.Valid(tok) // checked before hashing: any length is refused in O(1)
	if !valid {
		tok = dummyToken
	}
	body, ok := s.o.Stats.Me(pilot.Hash(tok)) // the game sees only the hash
	if !valid || !ok {
		writeJSON(w, http.StatusOK, noPilot)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// apiNotFound answers any other /api/ path in JSON.
func (s *Server[S, M, In, X]) apiNotFound(w http.ResponseWriter, r *http.Request) {
	if !s.apiAllow(w, r) {
		return
	}
	writeJSON(w, http.StatusNotFound, errorJSON(msgNoAPI))
}
