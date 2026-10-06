# roomkit

The multiplayer plumbing behind [Dogfight](https://github.com/ahmetbir/dogfight): rooms, lobby,
quick play, a hardened WebSocket server, rate limits, pilot tokens, blue/green drain, metrics, a
load-test harness, and the browser half (reconnecting socket, input shaper, client prediction
and reconciliation, interpolation, i18n, small UI primitives).

A game supplies its simulation, its messages and its screens by implementing a few interfaces;
roomkit runs everything around them.

- **Go:** `go get github.com/ahmetbir/roomkit@latest` — Go 1.26, one dependency
  ([coder/websocket](https://github.com/coder/websocket)).
- **TypeScript:** `"roomkit": "github:ahmetbir/roomkit#v0.1.0"` in `package.json`, then
  `import { Socket } from "roomkit/net/socket"`. Ships compiled ES2022 with type declarations; no
  runtime dependencies, no `three`.

## What is in it

| Go package | Does |
|---|---|
| `room` | The room actor: one goroutine at a fixed tick (60 Hz) that owns a game, seats, per-player input queues with seq/ack, ping, quick chat, timeouts, panic recovery, the lobby summary. A game implements `room.Game`. |
| `lobby` | Room codes, listing, quick play, room caps, drain, stats flush. |
| `server` | HTTP + WebSocket server: handshake, origin check, per-address and per-/48 connection gates, create/join/API rate limits, inbound guard, security headers and CSP, fingerprinted static assets, `/healthz`, `/api/rooms`, `/api/leaderboard`, `/api/me`. A game implements `server.Kit` (and optionally `server.Stats`). |
| `netproto` | The shared wire envelope (`t`, `v`, `name`, `tok`, `code`, `seq`, `ts`, `id`), core message types, error and API codes, `CleanName`. |
| `wsconn` | A connection whose `Send` never blocks: bounded queue, evictable snapshots, write timeout. |
| `limit` | Token buckets, keyed tables with refunds, connection gates. |
| `pilot` | Anonymous pilot tokens: issue, validate, hash; log redaction. |
| `metrics` | Prometheus text exposition with a per-game namespace. |
| `drain` | `SIGUSR1` drain / `SIGUSR2` undrain for blue/green deploys, with a stats-store handoff hook. |
| `loadtest` | A load-test harness (plan, ramp, histograms, gap detection) driven by a game's `Script`. |

| TS module | Does |
|---|---|
| `net/socket` | Reconnecting socket: backoff, `1012` rejoin, fatal codes, ping. |
| `net/shaper` | Outbound shaping: newest-input-only under backpressure, one-shot merges, gapped picks. |
| `net/codes`, `net/code`, `net/pilot` | Error codes, room-code helpers, pilot-token storage. |
| `predict/reconcile`, `predict/interp`, `predict/sessionmodel` | Client prediction with replay, server clock, interpolation buffer. |
| `i18n/*` | Dictionaries, plurals, params, live labels, formatting. |
| `ui/*` | `h`/`fill`/`text` DOM helpers, modal key handling, error card, banner, language toggle, key labels. |
| `audio/shell` | Lazy `AudioContext`, suspend while hidden, master gain + limiter. |
| `store` | Namespaced `localStorage` slots. |

## Develop

```sh
npm install          # TypeScript + node types (dev only)
bash scripts/check.sh   # go vet, go test -race, tsc, node tests, dist/ is current
```

`dist/` is committed (consumers' `node --test` cannot strip types inside `node_modules`). After
changing `ts/`, run `npm run build` and commit `dist/` with it; `check.sh` fails otherwise.

Agents and contributors: read [AGENTS.md](AGENTS.md).

## License

MIT
