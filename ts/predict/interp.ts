// Interpolation of remote entities in server time, generic over the state.

/** Blends two states; u in [0, 1]. */
export type Mix<S> = (a: S, b: S, u: number) => S;
/** Carries a state along its own motion for the given seconds. */
export type Ahead<S> = (s: S, seconds: number) => S;

const DEFAULT_TICK_MS = 1000 / 60;
const DEFAULT_DELAY_MS = 100;
const RELAX_EVERY_MS = 5000;
const RELAX_MS = 5;
const MAX_ENTRIES = 32;

type Entry<S> = { t: number; fs: S };

export class InterpBuffer<S> {
  private entries: Entry<S>[] = [];
  private readonly mix: Mix<S>;

  constructor(mix: Mix<S>) {
    this.mix = mix;
  }

  /** Adds a snapshot state; out-of-order or duplicate times are ignored. */
  push(serverTimeMs: number, fs: S): void {
    const last = this.entries.at(-1);
    if (last && serverTimeMs <= last.t) return;
    this.entries.push({ t: serverTimeMs, fs });
    if (this.entries.length > MAX_ENTRIES) this.entries.shift();
  }

  /** The latest entry (server time ms and state). */
  newest(): Entry<S> | undefined {
    return this.entries.at(-1);
  }

  size(): number {
    return this.entries.length;
  }

  /** The mix of the neighbours at renderTimeMs; clamps at the ends. */
  sample(renderTimeMs: number): S | null {
    const e = this.entries;
    if (e.length === 0) return null;
    if (renderTimeMs <= e[0].t) return e[0].fs;
    const last = e[e.length - 1];
    if (renderTimeMs >= last.t) return last.fs;
    let i = 1;
    while (e[i].t < renderTimeMs) i++;
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
export function extrapolate<S>(buf: InterpBuffer<S>, serverTimeMs: number, ahead: Ahead<S>): S | null {
  const last = buf.newest();
  if (!last || serverTimeMs <= last.t) return buf.sample(serverTimeMs);
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
  private offset = NaN;
  private relaxedAt = 0;
  private readonly tickMs: number;
  private readonly delayMs: number;

  constructor(tickMs = DEFAULT_TICK_MS, delayMs = DEFAULT_DELAY_MS) {
    this.tickMs = tickMs;
    this.delayMs = delayMs;
  }

  /** Records a snapshot of tick received at local time nowMs. */
  observe(tick: number, nowMs: number): void {
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
  serverMs(tick: number): number {
    return tick * this.tickMs;
  }

  /** Server time to render remote entities at. */
  renderTime(nowMs: number): number {
    return nowMs - this.offset - this.delayMs;
  }
}
