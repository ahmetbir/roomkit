// Client-side prediction with server reconciliation, generic over the state S,
// the input I and the environment E. A game supplies its fixed step (Model)
// and how a correction is shown (Smoother).

const MAX_PENDING = 120; // 2 s of inputs; older ones are dropped if acks stall
const MAX_LAG = 8;       // server steps behind the acked inputs absorbed as timing noise
const BASE_WINDOW = 30;  // snapshots (~1 s) whose median offset is the baseline

/** One fixed step of the game's simulation, ending at world tick `tick`. */
export type Model<S, I, E> = { step(s: S, inp: I, env: E, tick: number): S };

/** How a jump of the physics state is drawn. */
export interface Smoother<S> {
  rebase(drawnFrom: S, to: S): void; // the physics state jumps: keep drawing the old one, then fade
  draw(s: S, dtS: number): S;        // s plus the decaying correction; dtS = frame time
  reset(): void;
}

type Pending<I> = { seq: number; inp: I };

export class Reconciler<S, I, E> {
  private readonly model: Model<S, I, E>;
  private readonly smoother: Smoother<S>;
  private pending: Pending<I>[] = [];
  private phys: S;
  // Server step count vs acked inputs. The server repeats the last input
  // when its queue starves (one step more) and drops a backlog (steps
  // fewer), so d = snapTick − ack wanders by a tick or two under jitter.
  // base follows the median of d over the last ~1 s (it stays while inside
  // the middle half of those values); shift = d − base is that
  // short noise only. The server state is then the client's own history
  // `shift` steps later in time, not a different place: replay skips (or
  // repeats) that many steps. A lasting change of d (latency step, tab
  // hidden) moves the median once it leaves the middle half (~0.75 s for a
  // clean step) and is corrected once.
  private base = NaN;
  private recent: number[] = []; // last BASE_WINDOW values of d
  private shift = 0;
  private snapTick = 0;
  private ack = 0;
  private lastAcked: I | null = null;
  private lastSeq = 0;

  constructor(model: Model<S, I, E>, smoother: Smoother<S>, initial: S) {
    this.model = model;
    this.smoother = smoother;
    this.phys = initial;
  }

  /** World tick the step of input seq ends on, by the latest snapshot (the wind's clock). */
  tickFor(seq: number): number {
    return this.snapTick + seq - this.ack - this.shift;
  }

  /** Records a sent input and advances the predicted state by one tick (the step to world tick `tick`). */
  push(seq: number, inp: I, tick: number, env: E): void {
    this.pending.push({ seq, inp });
    this.lastSeq = seq;
    if (this.pending.length > MAX_PENDING) this.pending.shift();
    this.phys = this.model.step(this.phys, inp, env, tick);
  }

  /**
   * Rebases prediction on the server state of world tick snapTick, on which
   * the server applied seq ack: replays the inputs after ack, seq n as the
   * step to tickFor(n) (by seq, so inputs dropped past the cap do not shift
   * the wind ticks). Steps the server took beyond its acked inputs (shift)
   * are skipped from the replay, steps it dropped are replayed with the last
   * acked input, so queue timing noise does not move the drawn plane.
   */
  reconcile(server: S, ack: number, snapTick: number, env: E): void {
    for (const p of this.pending) if (p.seq <= ack) this.lastAcked = p.inp;
    this.pending = this.pending.filter((p) => p.seq > ack);
    const d = snapTick - ack;
    this.recent.push(d);
    if (this.recent.length > BASE_WINDOW) this.recent.shift();
    const q = quartiles(this.recent);
    // Follow lasting changes only: inside the middle half of recent d the base stays.
    if (Number.isNaN(this.base) || this.base < q[0] || this.base > q[2]) this.base = q[1];
    let shift = d - this.base;
    // Beyond what the replay can absorb the change is real: re-anchor on it
    // and take the whole correction now.
    if (shift > this.pending.length || shift < -MAX_LAG) {
      this.recent = [d];
      this.base = d;
      shift = 0;
    }
    this.snapTick = snapTick;
    this.ack = ack;
    this.shift = shift;
    let corrected = server;
    // Steps the server dropped: replayed with the last acked input (the first
    // pending one right after a spawn, before anything was acked).
    const repeat = this.lastAcked ?? this.pending[0]?.inp ?? null;
    for (let j = 1; j <= -shift && repeat !== null; j++) {
      corrected = this.model.step(corrected, repeat, env, snapTick + j);
    }
    for (const p of this.pending) {
      if (p.seq <= ack + shift) continue;
      corrected = this.model.step(corrected, p.inp, env, this.tickFor(p.seq));
    }
    this.smoother.rebase(this.phys, corrected);
    this.phys = corrected;
  }

  /** World ticks the predicted state runs ahead of the latest snapshot's. */
  ahead(): number {
    return Math.max(0, this.lastSeq - this.ack - this.shift);
  }

  /** Unacknowledged inputs kept for replay. */
  pendingCount(): number {
    return this.pending.length;
  }

  /** Predicted physics state. */
  state(): S {
    return this.phys;
  }

  /** Physics state plus the decaying visual correction; dtS = frame time. */
  render(dtS: number): S {
    return this.smoother.draw(this.phys, dtS);
  }

  /** Hard reset on spawn / death; snapTick and ack are the spawn snapshot's (baseline of the shift). */
  reset(s: S, snapTick?: number, ack?: number): void {
    this.pending = [];
    this.recent = snapTick !== undefined && ack !== undefined ? [snapTick - ack] : [];
    this.base = this.recent.length ? this.recent[0] : NaN;
    this.shift = 0;
    this.snapTick = snapTick ?? 0;
    this.ack = ack ?? 0;
    this.lastAcked = null;
    this.lastSeq = this.ack;
    this.phys = s;
    this.smoother.reset();
  }
}

/** Lower quartile, lower median and upper quartile of a non-empty list. */
function quartiles(xs: number[]): [number, number, number] {
  const s = [...xs].sort((a, b) => a - b);
  const at = (f: number) => s[Math.floor((s.length - 1) * f)];
  return [at(0.25), at(0.5), s[Math.ceil((s.length - 1) * 0.75)]];
}
