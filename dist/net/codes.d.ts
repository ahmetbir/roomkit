export declare const ERROR_CODES: readonly ["version", "no_room", "bad_room", "full", "bad_msg", "no_create", "busy", "creates", "joins", "flood", "conns", "timeout", "updating"];
export type ErrorCode = (typeof ERROR_CODES)[number];
/** The client's own code for "no_room" right after a server update (the room was lost to it). */
export declare const ROOM_GONE = "room_gone";
/** Errors that end only this connection (the server's flood kick): the socket reconnects instead of giving up. */
export declare const RECOVERABLE: Set<string>;
export declare function isErrorCode(c: unknown): c is ErrorCode;
