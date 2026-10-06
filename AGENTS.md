# Notes for agents and contributors

roomkit is game-agnostic multiplayer infrastructure. Its consumers are separate repositories
(Dogfight; an F1 racing game). Every change here is a change to all of them: keep it small,
tested, and backward compatible within a minor version.

## Setup and checks

- Go 1.26 (`toolchain` in `go.mod`), Node 22 (`node --experimental-strip-types` runs the TS
  tests directly; Node 20 cannot).
- `npm install`, then `bash scripts/check.sh` before every commit. It runs `go vet`,
  `go test -race`, `tsc --noEmit`, the node tests, and fails if the committed `dist/` differs from
  a fresh build.
- After touching `ts/`: `npm run build` and commit `dist/` together with the source.

## Testing a change against a game

A game pins a tag (`require github.com/ahmetbir/roomkit vX.Y.Z`, `"roomkit":
"git+https://github.com/ahmetbir/roomkit.git#vX.Y.Z"`). To try unreleased roomkit code in a game checkout beside this
one, temporarily point it here, on a local branch only:

```sh
go mod edit -replace github.com/ahmetbir/roomkit=../roomkit
(cd client && npm install ../../roomkit)
```

Run the game's goldens and smoke test (`scripts/smoke.sh` in Dogfight). Never push a game commit
with a `replace` or a `file:` dependency; Dogfight's `release.sh` refuses one. Then tag roomkit
(`vX.Y.Z`, minor for new API, patch for fixes) and bump the game.

## Invariants

### Concurrency

- **A room is one actor goroutine.** Every `room.Game` method runs on it and must not block, do
  I/O, start goroutines or keep the `Outbox`.
- **Nothing blocks the room on a client.** `wsconn.Conn.Send` never blocks: 64-message queue,
  the oldest evictable snapshot goes first (its events carried into the next, `wsconn.Carrier`),
  other messages are never evicted, a queue full for 2 s closes the connection. `Close` never
  blocks; `Wait` does (never call it on an actor goroutine).
- `server.Kit` methods and `Msg.Latch` run on connection and HTTP goroutines: keep them pure.
  `server.Stats` must be safe for concurrent use.
- Cross-goroutine state: the room summary (atomic pointer), the lobby map (mutex), metrics
  (atomics), channels. No globals.

### Wire

- Flat JSON objects with a `"t"` field. Core owns the fields in `netproto.Header`; everything else
  is the game's. A snapshot starts `{"t":"snap","tick":N` (loadtest and the client clock read it).
- Error and API codes live in `netproto/codes.go` and `ts/net/codes.ts`, pinned by
  `TestErrorCodesMatchClientCore`. Never rename or reuse a code; add new ones. Server `msg` texts
  stay Turkish for old clients.
- Other Go↔TS pins (`internal/tsconst` reads `ts/`): welcome fields, restart close code, ping
  interval under the idle timeout, input queue sizes.

### Rate limits and the guard

- Inputs (`in`): own bucket, 90/s burst 120, only ever dropped (one-shot presses latch into the
  next input), never kicked.
- `ping` and `ClassChoice` game types: 2/s burst 4, then the shared bucket; everything else: the
  shared bucket (90/s burst 120). These refusals close with `flood`.
- Ceiling before decoding: >300 msg/s or >64 KB/s over 5 s ends the connection; frames are capped
  at 2048 bytes.
- A game cannot reclassify core types (`hello`, `create`, `join`, `quick`, `in`, `ping`, `chat`).

### Security posture (a game cannot weaken it)

- Origin check by the WebSocket library, never skipped. `X-Real-IP` trusted only from configured
  proxy CIDRs. Per-address limits key on one IPv4 or one IPv6 /64; a /48 shares an aggregate cap.
- `/api/me` gives one body for missing, invalid, unknown or oversized tokens; only the token's
  SHA-256 is kept, the raw token never reaches logs, metrics, URLs or disk.
- Static files: fixed bundle names are rewritten to content-hashed URLs (`server/assets.go`);
  hashed URLs are `immutable`, bare names `no-cache`.

### Drain

`SIGUSR1`: rooms keep playing, tallies flush, the stats handoff releases its lock, new sockets get
`1012`, `/api/*` answers 503; exit when the last socket closes or after the max. `SIGUSR2`
undrains. Clients treat `1012` as "updating" and rejoin the same code.

### Game seams

- `room.Info.Bots` is what the bot gauge shows: the game counts its own bots (it need not be
  `Seats - Humans`). `Seats` is the capacity quick play compares `Humans` against.
- `room.Refuse(code)` from `Game.Join` turns a player away with the game's own code (shape
  `^[a-z][a-z0-9_]{0,31}$`, never a core code; otherwise the player gets `full` and the server
  logs the bug). The error frame carries the code as `code` and `msg`; quick play skips such a
  room like a full one. Keep refusal codes in the game's notice-code collision test.
- `server.Stats.Boards()` lists `BoardID{Period, Key}` pairs; `/api/leaderboard?period=&key=`
  serves only those (a missing `key` is `""`), anything else is `bad_period`.
- `ledger.Store[D, R]` is Dogfight's stats mechanics without its schema: `Record` never blocks
  (full or closed = dropped and counted), one actor owns the maps, `stats.lock` is held until
  `Close` (a second `Open` gets `ErrLocked`). `Schema.Fold` and `Key` are replayed from the journal
  after a crash, so keep them deterministic; `D` and `R` must JSON-marshal. Eviction is
  least-recently-seen; game-specific eviction or admission rules belong in `Schema.Empty`/`Key`.

### Testing

- `internal/fakegame` + `internal/fakekit` run a tiny game through the real room, lobby and server.
  New core behaviour gets a test there, not only in a consumer.
- Prefer `testing/synctest` for anything timed. Netcode that passes with instant fakes can still
  race under delay: add latency before calling it clean.

## Making a change

1. Change roomkit with tests; `bash scripts/check.sh`.
2. Prove it in each consumer with the local `replace`/`file:` pair (goldens unchanged unless the
   change is meant to alter the wire, which needs a protocol-version bump in that game).
3. Commit, tag, bump the consumers.
