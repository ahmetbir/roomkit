import { RECOVERABLE, ROOM_GONE } from "./codes.js";
import { TOKEN_RE } from "./pilot.js";
import { Shaper } from "./shaper.js";
export const RECONNECT_MS = [500, 1000, 2000, 4000, 8000];
/** Dials before the first welcome until the socket gives up (~15 s of backoff). */
export const FIRST_TRIES = 6;
export const PING_MS = 15000; // server idle-closes after 30 s without input (pinned by core/wsconn crosslang_test.go)
const OPEN = 1;
/** WebSocket close code of a draining server (Service Restart): reconnect. Pinned by core/wsconn crosslang_test.go. */
export const CLOSE_RESTART = 1012;
/** The server's error code when the rejoined room does not exist; ROOM_GONE replaces it right after an update. */
const NO_ROOM = "no_room";
export function socketURL(loc) {
    return `${loc.protocol === "https:" ? "wss" : "ws"}://${loc.host}/ws`;
}
const errCode = (m) => m.code ?? "";
const browserEnv = () => ({
    dial: (url) => new WebSocket(url),
    now: () => performance.now(),
    setTimeout: (f, ms) => setTimeout(f, ms),
    clearTimeout: (h) => clearTimeout(h),
    setInterval: (f, ms) => setInterval(f, ms),
    clearInterval: (h) => clearInterval(h),
});
/**
 * Socket owns the connection and its handshake: hello + create|join|quick on the
 * first open; after a welcome it remembers the room code, so every reconnect
 * sends hello + join(code) and enters the room as a new player (spec §6).
 * A server "error" is final: the socket closes and reports it via onFatal.
 * C is the game's client message type, S its server message type.
 */
export class Socket {
    conn = null;
    entry;
    attempt = 0;
    joined = false;
    welcomed = false; // ever, on any connection: from then on, retry forever
    stopped = false;
    updated = false; // a 1012 close since the last welcome
    retryTimer = null;
    ping = null;
    shaper = null; // per connection, from onopen
    tok = "";
    url;
    name;
    h;
    o;
    env;
    constructor(url, name, entry, h, o, env = browserEnv()) {
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
    code() {
        return this.entry.t === "join" ? this.entry.code : "";
    }
    /** Pilot token for every later hello; invalid tokens are ignored. */
    setToken(tok) {
        if (TOKEN_RE.test(tok))
            this.tok = tok;
    }
    /**
     * Sends m once the handshake is done and reports whether it was written.
     * Nothing is queued: while reconnecting, messages are dropped. Inputs
     * and picks pass the connection's Shaper (the game's policy).
     */
    send(m) {
        return this.joined && !!this.shaper?.send(m);
    }
    /** Starts over after onUnreachable (or any stop): dials again now. */
    retry() {
        if (!this.stopped)
            return;
        this.stopped = false;
        this.attempt = 0;
        this.h.onStatus("connecting");
        this.connect();
    }
    /** Closes the connection and stops reconnecting. */
    close() {
        if (this.stopped)
            return;
        this.stop();
        this.h.onStatus("closed");
    }
    connect() {
        let c;
        try {
            c = this.env.dial(this.url);
        }
        catch {
            this.retryLater(); // e.g. SecurityError from new WebSocket
            return;
        }
        this.conn = c;
        c.onopen = () => {
            this.shaper = new Shaper({
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
    receive(data) {
        if (typeof data !== "string")
            return;
        let m;
        try {
            m = JSON.parse(data);
        }
        catch {
            return;
        }
        if (typeof m !== "object" || m === null)
            return;
        this.shaper?.received();
        switch (m.t) {
            case "error":
                if (RECOVERABLE.has(errCode(m)) && this.conn) {
                    this.lost(this.conn); // the reconnect banner, then a fresh connection
                    return;
                }
                this.stop();
                this.h.onStatus("closed");
                this.h.onFatal(this.updated && errCode(m) === NO_ROOM ? ROOM_GONE : errCode(m), m.msg ?? "");
                return;
            case "welcome":
                this.updated = false;
                this.entry = { t: "join", code: m.code };
                this.attempt = 0;
                this.joined = true;
                this.welcomed = true;
                this.h.onStatus("open");
                break;
        }
        this.h.onMsg(m);
    }
    lost(c, code) {
        if (c !== this.conn || this.stopped)
            return;
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
    retryLater() {
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
    write(m) {
        if (!this.conn || this.conn.readyState !== OPEN)
            return false;
        this.conn.send(JSON.stringify(m));
        return true;
    }
    drop() {
        this.joined = false;
        if (this.ping !== null)
            this.env.clearInterval(this.ping);
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
    stop() {
        this.stopped = true;
        if (this.retryTimer !== null)
            this.env.clearTimeout(this.retryTimer);
        this.retryTimer = null;
        this.drop();
    }
}
