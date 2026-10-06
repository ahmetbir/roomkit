// Outbound rate shaping for one connection: a stalled network must not
// release seconds of queued inputs in one bunch, and some messages keep a gap.
// What counts as an input and as gapped is the game's ShaperPolicy.

/** Every message has a type tag. */
export type Envelope = { t: string };

/** The game's outbound shaping rules. */
export type ShaperPolicy<M extends Envelope> = {
  isInput(m: M): boolean;    // newest-wins while the connection is backed up
  latch(held: M, next: M): M; // next plus held's one-shot presses
  gapped(m: M): boolean;     // at most one per gapMs; the newest waiting one is sent
  gapMs: number;
};

/** Bytes queued in the browser above which the connection counts as backed up. */
export const MAX_BUFFERED = 8 * 1024;
/** Server silence (snaps come at 30 Hz) after which the network counts as stalled. */
export const STALL_MS = 1000;

export type ShaperEnv<M extends Envelope> = {
  now: () => number;
  setTimeout: (f: () => void, ms: number) => unknown;
  clearTimeout: (h: unknown) => void;
  buffered: () => number;              // the socket's bufferedAmount
  write: (m: M) => boolean;
};

/**
 * Shaper sits between the game and one open connection. While the connection
 * is backed up it holds only the newest input (one-shot presses of the ones
 * it replaces carry over) and sends it once traffic flows again; the server
 * keeps only the newest inputs anyway. Gapped messages go out at most once
 * per policy.gapMs. Every message it accepts reports true.
 */
export class Shaper<M extends Envelope> {
  private readonly env: ShaperEnv<M>;
  private readonly p: ShaperPolicy<M>;
  private lastRecv: number;
  private heldIn: M | null = null;
  private lastGap = -Infinity;
  private heldGap: M | null = null;
  private gapTimer: unknown = null;

  constructor(env: ShaperEnv<M>, policy: ShaperPolicy<M>) {
    this.env = env;
    this.p = policy;
    this.lastRecv = env.now();
  }

  /** Any server message: the network is flowing; a held input goes out. */
  received(): void {
    this.lastRecv = this.env.now();
    if (this.heldIn && !this.backedUp()) {
      const m = this.heldIn;
      this.heldIn = null;
      this.env.write(m);
    }
  }

  send(m: M): boolean {
    if (this.p.isInput(m)) return this.input(m);
    if (this.p.gapped(m)) return this.gap(m);
    return this.env.write(m);
  }

  /** The connection is gone: nothing held is sent. */
  dispose(): void {
    if (this.gapTimer !== null) this.env.clearTimeout(this.gapTimer);
    this.gapTimer = null;
    this.heldIn = this.heldGap = null;
  }

  private backedUp(): boolean {
    return this.env.buffered() > MAX_BUFFERED || this.env.now() - this.lastRecv > STALL_MS;
  }

  private input(m: M): boolean {
    const held = this.heldIn;
    const out = held ? this.p.latch(held, m) : m;
    if (this.backedUp()) {
      this.heldIn = out;
      return true;
    }
    this.heldIn = null;
    return this.env.write(out);
  }

  private gap(m: M): boolean {
    const wait = this.lastGap + this.p.gapMs - this.env.now();
    if (wait <= 0 && this.gapTimer === null) {
      this.lastGap = this.env.now();
      return this.env.write(m);
    }
    this.heldGap = m;
    if (this.gapTimer === null) {
      this.gapTimer = this.env.setTimeout(() => {
        this.gapTimer = null;
        const g = this.heldGap;
        this.heldGap = null;
        if (g) {
          this.lastGap = this.env.now();
          this.env.write(g);
        }
      }, Math.max(wait, 0));
    }
    return true;
  }
}
