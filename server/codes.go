package server

import "github.com/ahmetbir/roomkit/netproto"

// errCode is the protocol code of a user-facing error text (socket "error"
// messages and API error bodies); "" for a text without one.
func errCode(msg string) string {
	switch msg {
	case msgVersion:
		return netproto.CodeVersion
	case msgNoRoom:
		return netproto.CodeNoRoom
	case msgBadRoom:
		return netproto.CodeBadRoom
	case msgFull:
		return netproto.CodeFull
	case msgBad:
		return netproto.CodeBad
	case msgNoCreate:
		return netproto.CodeNoCreate
	case msgBusy:
		return netproto.CodeBusy
	case msgCreates:
		return netproto.CodeCreates
	case msgJoins:
		return netproto.CodeJoins
	case msgFlood:
		return netproto.CodeFlood
	case msgConns:
		return netproto.CodeConns
	case msgTimeout:
		return netproto.CodeTimeout
	case msgUpdating:
		return netproto.CodeUpdating
	case msgStatsOff:
		return netproto.CodeStatsOff
	case msgBadPeriod:
		return netproto.CodeBadPeriod
	case msgNoAPI:
		return netproto.CodeNotFound
	case msgRequests:
		return netproto.CodeRate
	}
	return ""
}
