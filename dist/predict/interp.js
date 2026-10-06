// Interpolation of remote entities in server time, generic over the state.
const DEFAULT_TICK_MS = 1000 / 60;
const DEFAULT_DELAY_MS = 100;
const RELAX_EVERY_MS = 5000;
const RELAX_MS = 5;
const MAX_ENTRIES = 32;
export class InterpBuffer {
    entries = [];
    mix;
    constructor(mix) {
        this.mix = mix;
    }
    /** Adds a snapshot state; out-of-order or duplicate times are ignored. */
    push(serverTimeMs, fs) {
        const last = this.entries.at(-1);
        if (last && serverTimeMs <= last.t)
            return;
        this.entries.push({ t: serverTimeMs, fs });
        if (this.entries.length > MAX_ENTRIES)
            this.entries.shift();
    }
    /** The latest entry (server time ms and state). */
    newest() {
        return this.entries.at(-1);
    }
    size() {
        return this.entries.length;
    }
    /** The mix of the neighbours at renderTimeMs; clamps at the ends. */
    sample(renderTimeMs) {
        const e = this.entries;
        if (e.length === 0)
            return null;
        if (renderTimeMs <= e[0].t)
            return e[0].fs;
        const last = e[e.length - 1];
        if (renderTimeMs >= last.t)
            return last.fs;
        let i = 1;
        while (e[i].t < renderTimeMs)
            i++;
        const a = e[i - 1], b = e[i];
        const u = (renderTimeMs - a.t) / (b.t - a.t);
        return this.mix(a.fs, b.fs, u);
    }
}
const MAX_AHEAD_MS = 500; // extrapolation horizon past the newest snapshot
/**
 * The state at serverTimeMs: interpolated inside the buffer, carried along
 * the newest state's own motion past its end (at most MAX_AHEAD_MS), e.g. a
 * target's present position for a gunsight while the drawn one lags behind.
 */
export function extrapolate(buf, serverTimeMs, ahead) {
    const last = buf.newest();
    if (!last || serverTimeMs <= last.t)
        return buf.sample(serverTimeMs);
    const s = Math.min(serverTimeMs - last.t, MAX_AHEAD_MS) / 1000;
    return ahead(last.fs, s);
}
/**
 * ServerClock maps local time to server time. The offset tracks the lowest
 * observed delay (now − serverMs) and relaxes by 5 ms every 5 s so it can
 * follow a slowly rising delay. tickMs is the game's tick length, delayMs
 * how far behind the server remote entities are drawn.
 */
export class ServerClock {
    offset = NaN;
    relaxedAt = 0;
    tickMs;
    delayMs;
    constructor(tickMs = DEFAULT_TICK_MS, delayMs = DEFAULT_DELAY_MS) {
        this.tickMs = tickMs;
        this.delayMs = delayMs;
    }
    /** Records a snapshot of tick received at local time nowMs. */
    observe(tick, nowMs) {
        const d = nowMs - tick * this.tickMs;
        if (Number.isNaN(this.offset)) {
            this.offset = d;
            this.relaxedAt = nowMs;
            return;
        }
        while (nowMs - this.relaxedAt >= RELAX_EVERY_MS) {
            this.offset += RELAX_MS;
            this.relaxedAt += RELAX_EVERY_MS;
        }
        this.offset = Math.min(this.offset, d);
    }
    /** Server time (ms) for a tick. */
    serverMs(tick) {
        return tick * this.tickMs;
    }
    /** Server time to render remote entities at. */
    renderTime(nowMs) {
        return nowMs - this.offset - this.delayMs;
    }
}
