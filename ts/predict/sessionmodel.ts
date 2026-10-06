// Test model of the server session queue (core/room/queue.go, sizes pinned by its crosslang_test.go; used by the prediction tests only):
// inputs with seq <= the last accepted one are dropped, a full queue (8)
// loses its oldest entry, a backlog above 4 is dropped at tick time, and a
// starved queue repeats the last input (ack does not move).
const QUEUE_CAP = 8;
const QUEUE_KEEP = 4;

export class SessionModel<I> {
  private q: { seq: number; inp: I }[] = [];
  private last: I | null = null;
  private lastSeq = 0;
  ack = 0;

  push(seq: number, inp: I): void {
    if (seq <= this.lastSeq) return;
    this.lastSeq = seq;
    if (this.q.length === QUEUE_CAP) this.q.shift();
    this.q.push({ seq, inp });
  }

  /** The input for this tick and whether it is a fresh one (false: repeat or none yet). */
  next(): { inp: I | null; fresh: boolean } {
    if (this.q.length === 0) return { inp: this.last, fresh: false };
    if (this.q.length > QUEUE_KEEP) this.q.splice(0, this.q.length - QUEUE_KEEP);
    const e = this.q.shift()!;
    this.ack = e.seq;
    this.last = e.inp;
    return { inp: e.inp, fresh: true };
  }
}
