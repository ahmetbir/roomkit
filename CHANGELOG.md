# Changelog

Minor versions add API; patch versions fix. Every release is additive for existing games.

## v0.4.0

- `room.Who.Addr` (`netip.Addr`): the client address as the per-address limits derive it
  (`X-Real-IP` only from `TrustProxy` peers, IPv4-mapped unmapped), set in the handshake; zero
  when unknown. For the game's own records; never sent to a client.
- `server.Admitter`, an optional Kit interface: `Admit(req server.AdmitRequest) (code string,
  ok bool)`, with `AdmitRequest{Who, Kind ("quick"|"create"|"join"), Code (join's room code)}`,
  runs after hello and the drain check, before quick play, create or join-by-code touches a room.
  A refusal is sent like a `room.Refuse` code (same validation); no room is created, joined or
  counted against the room cap, and no create or join token is spent. It counts as the new
  `admit` reject reason and is logged by code and kind (no names). Fixes the empty room (and
  burnt room cap) a `Game.Join` refusal left behind during quick/create.
- `room.Info.NoQuick`: quick play skips the room; listing, caps, metrics and join-by-code are
  unchanged.

## v0.3.0

- `ledger`: a generic journaled stats store (Dogfight's mechanics without its schema).

## v0.2.0

- Refusal codes (`room.Refuse`), reported bots (`room.Info.Bots`), leaderboard keys
  (`server.BoardID`).

## v0.1.0

- Extracted from Dogfight; behaviour unchanged.
