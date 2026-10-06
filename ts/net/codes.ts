// Core error codes (core/netproto/codes.go); core/netproto TestErrorCodesMatchClientCore keeps the lists equal.
// The client shows the text of the code in its own language; the message's msg is a fallback.

export const ERROR_CODES = [
  "version", "no_room", "bad_room", "full", "bad_msg", "no_create", "busy", "creates", "joins", "flood", "conns", "timeout", "updating",
] as const;
export type ErrorCode = (typeof ERROR_CODES)[number];

/** The client's own code for "no_room" right after a server update (the room was lost to it). */
export const ROOM_GONE = "room_gone";

/** Errors that end only this connection (the server's flood kick): the socket reconnects instead of giving up. */
export const RECOVERABLE = new Set(["flood"]);

export function isErrorCode(c: unknown): c is ErrorCode {
  return (ERROR_CODES as readonly unknown[]).includes(c);
}
