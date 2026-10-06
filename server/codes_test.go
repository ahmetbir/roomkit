package server

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ahmetbir/roomkit/netproto"
)

// Every user-facing error text carries a code, every socket error code has a
// text, and an API error body names its code.
func TestEveryErrorTextHasACode(t *testing.T) {
	socket := []string{msgVersion, msgNoRoom, msgBadRoom, msgFull, msgBad, msgNoCreate, msgBusy, msgCreates, msgJoins,
		msgFlood, msgConns, msgTimeout, msgUpdating}
	var codes []string
	for _, m := range socket {
		c := errCode(m)
		if c == "" {
			t.Fatalf("%q has no code", m)
		}
		codes = append(codes, c)
	}
	if !slices.Equal(codes, netproto.ErrorCodes()) {
		t.Fatalf("socket codes %v, protocol %v", codes, netproto.ErrorCodes())
	}
	for _, m := range []string{msgStatsOff, msgBadPeriod, msgNoAPI, msgRequests} {
		var body map[string]string
		if err := json.Unmarshal(errorJSON(m), &body); err != nil || body["error"] != m || body["code"] == "" {
			t.Fatalf("errorJSON(%q) = %v (%v)", m, body, err)
		}
	}
	if errCode("x") != "" {
		t.Fatal("unknown text got a code")
	}
}
