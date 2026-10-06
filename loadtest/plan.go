package loadtest

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Slot is one simulated player's place in the run: its room (-1 = quick
// play) and whether it creates that room.
type Slot struct {
	Room    int
	Creator bool
}

// Assign spreads players over rooms round-robin, so a linear ramp fills
// every room evenly; the first player of each room creates it. rooms == 0
// sends every player through quick play.
func Assign(players, rooms int) []Slot {
	out := make([]Slot, players)
	for i := range out {
		if rooms <= 0 {
			out[i] = Slot{Room: -1}
			continue
		}
		out[i] = Slot{Room: i % rooms, Creator: i < rooms}
	}
	return out
}

// HumansPerRoom is the most humans any one room gets from Assign.
func HumansPerRoom(players, rooms int) int {
	if rooms <= 0 || players <= 0 {
		return 0
	}
	return (players + rooms - 1) / rooms
}

// StartAt is player i's connect time after the run starts: players start
// evenly over ramp (all at once with ramp 0).
func StartAt(i, n int, ramp time.Duration) time.Duration {
	if n <= 1 || ramp <= 0 {
		return 0
	}
	return time.Duration(int64(ramp) * int64(i) / int64(n))
}

// WSURL checks a target URL: ws:// or wss:// with a host; an empty path
// becomes the game socket path /ws.
func WSURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("scheme must be ws or wss, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}

// snapPrefix is how every snapshot starts (protocol.Snap's field order).
const snapPrefix = `{"t":"snap","tick":`

// SnapTick reads a snapshot's tick from its prefix without decoding the
// rest; ok is false for anything else (decode it in full).
func SnapTick(b []byte) (tick int, ok bool) {
	if !bytes.HasPrefix(b, []byte(snapPrefix)) {
		return 0, false
	}
	digits := 0
	for _, c := range b[len(snapPrefix):] {
		if c < '0' || c > '9' {
			break
		}
		tick = tick*10 + int(c-'0')
		digits++
	}
	return tick, digits > 0 && digits < 19
}
