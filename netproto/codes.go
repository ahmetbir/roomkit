package netproto

// Stable codes of the user-facing failures every game shares. The client
// shows its own text for a code; Msg stays the server's text for old clients.
const (
	// "error" messages (the connection ends).
	CodeVersion  = "version"
	CodeNoRoom   = "no_room"
	CodeBadRoom  = "bad_room"
	CodeFull     = "full"
	CodeBad      = "bad_msg"
	CodeNoCreate = "no_create"
	CodeBusy     = "busy"
	CodeCreates  = "creates"
	CodeJoins    = "joins"
	CodeFlood    = "flood"
	CodeConns    = "conns"
	CodeTimeout  = "timeout"
	CodeUpdating = "updating"

	// HTTP API errors ({"error", "code"}).
	CodeStatsOff  = "stats_off"
	CodeBadPeriod = "bad_period"
	CodeNotFound  = "not_found"
	CodeRate      = "rate"
)

// ErrorCodes are the codes an "error" message may carry, in the client's order.
func ErrorCodes() []string {
	return []string{CodeVersion, CodeNoRoom, CodeBadRoom, CodeFull, CodeBad, CodeNoCreate, CodeBusy, CodeCreates, CodeJoins,
		CodeFlood, CodeConns, CodeTimeout, CodeUpdating}
}

// APICodes are the codes of HTTP API error bodies.
func APICodes() []string { return []string{CodeStatsOff, CodeBadPeriod, CodeNotFound, CodeRate} }
