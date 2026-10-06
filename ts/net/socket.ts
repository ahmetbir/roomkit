import { RECOVERABLE, ROOM_GONE } from "./codes.ts";
import { TOKEN_RE } from "./pilot.ts";
import { Shaper, type Envelope, type ShaperPolicy } from "./shaper.ts";

/** The entry messages every game shares (the creator's own message is the game's). */
export type Join = { t: "join"; code: string };
export type Quick = { t: "quick" };
/** The handshake and keepalive messages the socket itself sends. */
export type Hello = { t: "hello"; v: number; name: string; tok?: string };
export type Ping = { t: "ping"; ts: number };
/** The envelope every game's welcome carries (core/netproto Welcome): code is kept to rejoin after a 1012. */
export type Welcome = { t: "welcome"; you: number; code: string; tok?: string };

/** A game's wire: its protocol version and its outbound shaping. */
export type SocketOpts<C extends Envelope> = { version: number; policy: ShaperPolicy<C> };

/**
 * "open" means the handshake finished (welcome received on this connection).
 * "updating": the server closed with 1012 (a new version took over); the
 * socket reconnects, as for "connecting".
 */
export type Status = "connecting" | "updating" | "open" | "closed";

export type Handlers<S extends Envelope> = {
  onMsg: (m: S) => void;       // every server message except "error"
  onStatus: (s: Status) => void;
  onFatal: (code: string, msg: string) => void; // server "error" (code: core/net/codes.ts): reconnecting has stopped
  onUnreachable?: () => void;          // the first connection never got a welcome; retry() starts over
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

export const RECONNECT_MS = [500, 1000, 2000, 4000, 8000];
/** Dials before the first welcome until the socket gives up (~15 s of backoff). */
export const FIRST_TRIES = 6;
export const PING_MS = 15000; // server idle-closes after 30 s without input (pinned by core/wsconn crosslang_test.go)
const OPEN = 1;
/** WebSocket close code of a draining server (Service Restart): reconnect. Pinned by core/wsconn crosslang_test.go. */
export const CLOSE_RESTART = 1012;
/** The server's error code when the rejoined room does not exist; ROOM_GONE replaces it right after an update. */
const NO_ROOM = "no_room";

export function socketURL(loc: { protocol: string; host: string }): string {
  return `${loc.protocol === "https:" ? "wss" : "ws"}://${loc.host}/ws`;
}

const errCode = (m: Envelope): string => (m as { code?: string }).code ?? "";

const browserEnv = (): Env => ({
  dial: (url) => new WebSocket(url),
  now: () => performance.now(),
  setTimeout: (f, ms) => setTimeout(f, ms),
  clearTimeout: (h) => clearTimeout(h as number),
  setInterval: (f, ms) => setInterval(f, ms),
  clearInterval: (h) => clearInterval(h as number),
});

/**
 * Socket owns the connection and its handshake: hello + create|join|quick on the
 * first open; after a welcome it remembers the room code, so every reconnect
 * sends hello + join(code) and enters the room as a new player (spec §6).
 * A server "error" is final: the socket closes and reports it via onFatal.
 * C is the game's client message type, S its server message type.
 */
export class Socket<C extends Envelope, S extends Envelope> {
  private conn: Conn | null = null;
  private entry: C | Join | Quick;
  private attempt = 0;
  private joined = false;
  private welcomed = false; // ever, on any connection: from then on, retry forever
  private stopped = false;
  private updated = false; // a 1012 close since the last welcome
  private retryTimer: unknown = null;
  private ping: unknown = null;
  private shaper: Shaper<C> | null = null; // per connection, from onopen
  private tok = "";
  private readonly url: string;
  private readonly name: string;
  private readonly h: Handlers<S>;
  private readonly o: SocketOpts<C>;
  private readonly env: Env;

  constructor(url: string, name: string, entry: C | Join | Quick, h: Handlers<S>, o: SocketOpts<C>, env: Env = browserEnv()) {
    this.url = url;
    this.name = name;
    this.entry = entry;
    this.h = h;
    this.o = o;
    this.env = env;
    this.h.onStatus("connecting");
    this.connect();
  }

  /** Room code after the first welcome, "" before. */
  code(): string {
    return this.entry.t === "join" ? (this.entry as Join).code : "";
  }

  /** Pilot token for every later hello; invalid tokens are ignored. */
  setToken(tok: string): void {
    if (TOKEN_RE.test(tok)) this.tok = tok;
  }

  /**
   * Sends m once the handshake is done and reports whether it was written.
   * Nothing is queued: while reconnecting, messages are dropped. Inputs
   * and picks pass the connection's Shaper (the game's policy).
   */
  send(m: C): boolean {
    return this.joined && !!this.shaper?.send(m);
  }

  /** Starts over after onUnreachable (or any stop): dials again now. */
  retry(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.attempt = 0;
    this.h.onStatus("connecting");
    this.connect();
  }

  /** Closes the connection and stops reconnecting. */
  close(): void {
    if (this.stopped) return;
    this.stop();
    this.h.onStatus("closed");
  }

  private connect(): void {
    let c: Conn;
    try {
      c = this.env.dial(this.url);
    } catch {
      this.retryLater(); // e.g. SecurityError from new WebSocket
      return;
    }
    this.conn = c;
    c.onopen = () => {
      this.shaper = new Shaper<C>({
        now: this.env.now, setTimeout: this.env.setTimeout, clearTimeout: this.env.clearTimeout,
        buffered: () => c.bufferedAmount ?? 0, write: (m) => this.write(m),
      }, this.o.policy);
      // A drop before the welcome resends create on reconnect; the first,
      // empty room is reaped by the lobby.
      this.write({ t: "hello", v: this.o.version, name: this.name, ...(this.tok ? { tok: this.tok } : {}) });
      this.write(this.entry);
      this.ping = this.env.setInterval(() => this.write({ t: "ping", ts: this.env.now() }), PING_MS);
    };
    c.onmessage = (ev) => this.receive(ev.data);
    c.onclose = (ev) => this.lost(c, ev?.code);
  }

  private receive(data: unknown): void {
    if (typeof data !== "string") return;
    let m: S;
    try {
      m = JSON.parse(data) as S;
    } catch {
      return;
    }
    if (typeof m !== "object" || m === null) return;
    this.shaper?.received();
    switch (m.t) {
      case "error":
        if (RECOVERABLE.has(errCode(m)) && this.conn) {
          this.lost(this.conn); // the reconnect banner, then a fresh connection
          return;
        }
        this.stop();
        this.h.onStatus("closed");
        this.h.onFatal(this.updated && errCode(m) === NO_ROOM ? ROOM_GONE : errCode(m), (m as { msg?: string }).msg ?? "");
        return;
      case "welcome":
        this.updated = false;
        this.entry = { t: "join", code: (m as unknown as Welcome).code };
        this.attempt = 0;
        this.joined = true;
        this.welcomed = true;
        this.h.onStatus("open");
        break;
    }
    this.h.onMsg(m);
  }

  private lost(c: Conn, code?: number): void {
    if (c !== this.conn || this.stopped) return;
    this.drop();
    if (code === CLOSE_RESTART && !this.updated) {
      // The old server is draining: the next dial reaches the new one. A
      // second 1012 in a row (both colors draining) backs off as usual.
      this.updated = true;
      this.attempt = 0;
      this.h.onStatus("updating");
      this.retryTimer = this.env.setTimeout(() => {
        this.retryTimer = null;
        this.connect();
      }, RECONNECT_MS[0]);
      return;
    }
    this.retryLater();
  }

  private retryLater(): void {
    if (!this.welcomed && this.attempt + 1 >= FIRST_TRIES) {
      this.stop(); // never reached the game: say so instead of spinning forever
      this.h.onStatus("closed");
      this.h.onUnreachable?.();
      return;
    }
    const ms = RECONNECT_MS[Math.min(this.attempt, RECONNECT_MS.length - 1)];
    this.attempt++;
    this.h.onStatus("connecting");
    this.retryTimer = this.env.setTimeout(() => {
      this.retryTimer = null;
      this.connect();
    }, ms);
  }

  private write(m: C | Join | Quick | Hello | Ping): boolean {
    if (!this.conn || this.conn.readyState !== OPEN) return false;
    this.conn.send(JSON.stringify(m));
    return true;
  }

  private drop(): void {
    this.joined = false;
    if (this.ping !== null) this.env.clearInterval(this.ping);
    this.ping = null;
    this.shaper?.dispose();
    this.shaper = null;
    const c = this.conn;
    this.conn = null;
    if (c) {
      c.onopen = c.onmessage = c.onclose = null;
      c.close();
    }
  }

  private stop(): void {
    this.stopped = true;
    if (this.retryTimer !== null) this.env.clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.drop();
  }
}
