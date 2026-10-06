import { type Envelope, type ShaperPolicy } from "./shaper.js";
/** The entry messages every game shares (the creator's own message is the game's). */
export type Join = {
    t: "join";
    code: string;
};
export type Quick = {
    t: "quick";
};
/** The handshake and keepalive messages the socket itself sends. */
export type Hello = {
    t: "hello";
    v: number;
    name: string;
    tok?: string;
};
export type Ping = {
    t: "ping";
    ts: number;
};
/** The envelope every game's welcome carries (core/netproto Welcome): code is kept to rejoin after a 1012. */
export type Welcome = {
    t: "welcome";
    you: number;
    code: string;
    tok?: string;
};
/** A game's wire: its protocol version and its outbound shaping. */
export type SocketOpts<C extends Envelope> = {
    version: number;
    policy: ShaperPolicy<C>;
};
/**
 * "open" means the handshake finished (welcome received on this connection).
 * "updating": the server closed with 1012 (a new version took over); the
 * socket reconnects, as for "connecting".
 */
export type Status = "connecting" | "updating" | "open" | "closed";
export type Handlers<S extends Envelope> = {
    onMsg: (m: S) => void;
    onStatus: (s: Status) => void;
    onFatal: (code: string, msg: string) => void;
    onUnreachable?: () => void;
};
/** The subset of the browser WebSocket the socket uses. */
export interface Conn {
    readonly readyState: number;
    readonly bufferedAmount?: number;
    onopen: ((ev: Event) => void) | null;
    onmessage: ((ev: MessageEvent) => void) | null;
    onclose: ((ev: CloseEvent) => void) | null;
    send(data: string): void;
    close(): void;
}
/** Timers and connection factory; injectable for tests. */
export type Env = {
    dial: (url: string) => Conn;
    now: () => number;
    setTimeout: (f: () => void, ms: number) => unknown;
    clearTimeout: (h: unknown) => void;
    setInterval: (f: () => void, ms: number) => unknown;
    clearInterval: (h: unknown) => void;
};
export declare const RECONNECT_MS: number[];
/** Dials before the first welcome until the socket gives up (~15 s of backoff). */
export declare const FIRST_TRIES = 6;
export declare const PING_MS = 15000;
/** WebSocket close code of a draining server (Service Restart): reconnect. Pinned by core/wsconn crosslang_test.go. */
export declare const CLOSE_RESTART = 1012;
export declare function socketURL(loc: {
    protocol: string;
    host: string;
}): string;
/**
 * Socket owns the connection and its handshake: hello + create|join|quick on the
 * first open; after a welcome it remembers the room code, so every reconnect
 * sends hello + join(code) and enters the room as a new player (spec §6).
 * A server "error" is final: the socket closes and reports it via onFatal.
 * C is the game's client message type, S its server message type.
 */
export declare class Socket<C extends Envelope, S extends Envelope> {
    private conn;
    private entry;
    private attempt;
    private joined;
    private welcomed;
    private stopped;
    private updated;
    private retryTimer;
    private ping;
    private shaper;
    private tok;
    private readonly url;
    private readonly name;
    private readonly h;
    private readonly o;
    private readonly env;
    constructor(url: string, name: string, entry: C | Join | Quick, h: Handlers<S>, o: SocketOpts<C>, env?: Env);
    /** Room code after the first welcome, "" before. */
    code(): string;
    /** Pilot token for every later hello; invalid tokens are ignored. */
    setToken(tok: string): void;
    /**
     * Sends m once the handshake is done and reports whether it was written.
     * Nothing is queued: while reconnecting, messages are dropped. Inputs
     * and picks pass the connection's Shaper (the game's policy).
     */
    send(m: C): boolean;
    /** Starts over after onUnreachable (or any stop): dials again now. */
    retry(): void;
    /** Closes the connection and stops reconnecting. */
    close(): void;
    private connect;
    private receive;
    private lost;
    private retryLater;
    private write;
    private drop;
    private stop;
}
