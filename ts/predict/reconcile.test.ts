import { test } from "node:test";
import assert from "node:assert/strict";
import { Reconciler, type Model, type Smoother } from "./reconcile.ts";
import { SessionModel } from "./sessionmodel.ts";

// A 1-D world: the state is a number, an input adds to it; env is unused.
const model: Model<number, number, null> = { step: (s, i) => s + i };

// offset = drawn − physics; rebase keeps drawing the old state, draw decays it.
class Fade implements Smoother<number> {
  offset = 0;
  jumps = 0;
  rebase(drawnFrom: number, to: number): void {
    if (drawnFrom !== to) this.jumps++;
    this.offset += drawnFrom - to;
  }
  draw(s: number, dtS: number): number {
    this.offset *= Math.exp(-dtS / 0.1);
    return s + this.offset;
  }
  reset(): void {
    this.offset = 0;
  }
}

const input = (i: number) => (i % 7) + 1;
const make = () => {
  const sm = new Fade();
  return { sm, r: new Reconciler(model, sm, 0) };
};

test("reconcile replays unacked inputs onto the server state exactly", () => {
  const { sm, r } = make();
  const server: number[] = [0]; // server[n] = after inputs 1..n
  const LAG = 6;
  for (let seq = 1; seq <= 120; seq++) {
    server.push(server[seq - 1] + input(seq));
    r.push(seq, input(seq), seq, null);
    if (seq % 2 === 0 && seq > LAG) {
      const ack = seq - LAG;
      r.reconcile(server[ack], ack, ack, null);
      assert.equal(r.state(), server[seq], `seq ${seq}`);
      assert.equal(r.render(0), server[seq]);
    }
  }
  assert.equal(sm.jumps, 0);
});

test("a server that repeats or drops inputs (bursty delivery) does not move the state", () => {
  for (const burst of [3, 6, 8]) {
    const { r } = make();
    const q = new SessionModel<number>();
    const inFlight: { at: number; seq: number }[] = [];
    const snaps: { at: number; s: number; ack: number; tick: number }[] = [];
    let server = 0;
    let worst = 0;
    for (let t = 1; t <= 600; t++) {
      r.push(t, 1, r.tickFor(t), null); // a steady input
      inFlight.push({ at: Math.ceil(t / burst) * burst + 2, seq: t });
      while (inFlight.length && inFlight[0].at <= t) q.push(inFlight.shift()!.seq, 1);
      const si = q.next().inp;
      if (si !== null) server += si;
      if (t % 2 === 0) snaps.push({ at: t + 3, s: server, ack: q.ack, tick: t });
      while (snaps.length && snaps[0].at <= t) {
        const s = snaps.shift()!;
        const before = r.state();
        r.reconcile(s.s, s.ack, s.tick, null);
        if (t > 120) worst = Math.max(worst, Math.abs(r.state() - before));
      }
    }
    assert.equal(worst, 0, `burst ${burst}: state moved by ${worst}`);
  }
});

test("a lasting latency step is corrected in a few jumps, then the state is steady again", () => {
  const { sm, r } = make();
  // Constant input 1: the server state at tick T is T. At T=120 its inputs stop arriving
  // for 20 ticks (it repeats the last one, the ack freezes), then flow again 20 ticks late.
  const jumpsAt: number[] = [];
  for (let seq = 1; seq <= 240; seq++) {
    r.push(seq, 1, seq, null);
    if (seq % 2 !== 0) continue;
    const ack = seq < 120 ? seq : Math.max(120, seq - 20);
    const before = sm.jumps;
    r.reconcile(seq, ack, seq, null);
    if (sm.jumps > before) jumpsAt.push(seq);
  }
  // The median of d moves once it leaves the middle half of the last ~1 s (the
  // re-anchor), so the corrections come after the step and stop well before the end.
  // The frozen-ack ramp re-anchors twice (+16 at 166, +4 at 182): deterministic.
  assert.deepEqual(jumpsAt, [166, 182]);
});

test("pending inputs are capped at 120 (no growth while acks stall)", () => {
  const { r } = make();
  for (let seq = 1; seq <= 1000; seq++) r.push(seq, 1, seq, null);
  assert.equal(r.pendingCount(), 120);
});

test("ahead counts the predicted ticks past the latest snapshot", () => {
  const { r } = make();
  r.reset(0, 100, 40);
  assert.equal(r.ahead(), 0);
  for (let seq = 41; seq <= 46; seq++) r.push(seq, 1, r.tickFor(seq), null);
  assert.equal(r.ahead(), 6);
  r.reconcile(0, 43, 103, null);
  assert.equal(r.ahead(), 3);
});

test("the model sees the world tick of each replayed step", () => {
  const ticks: number[] = [];
  const r = new Reconciler<number, number, null>({ step: (s, i, _e, tick) => (ticks.push(tick), s + i) }, new Fade(), 0);
  for (let seq = 1; seq <= 3; seq++) r.push(seq, 1, 10 + seq, null);
  ticks.length = 0;
  r.reconcile(0, 0, 10, null);
  assert.deepEqual(ticks, [11, 12, 13]);
});

test("a correction is handed to the smoother, drawn at the old state, then fades", () => {
  const { sm, r } = make();
  r.push(1, 1, 1, null);
  r.reconcile(11, 1, 1, null); // the server says 11, the client had 1
  assert.equal(r.state(), 11);
  assert.equal(r.render(0), 1);
  for (let i = 0; i < 18; i++) r.render(1 / 60);
  assert.ok(Math.abs(r.render(0) - 11) < 1);
  r.reset(5);
  assert.equal(sm.offset, 0);
  assert.equal(r.state(), 5);
});

test("steps the server dropped are replayed with the last acked input, also when it is 0", () => {
  // Every step advances the state by 1 whatever the input, so a skipped replay shows.
  const r = new Reconciler<number, number, null>({ step: (s, i) => s + i + 1 }, new Fade(), 0);
  r.reset(0, 100, 40); // d = 60
  for (let seq = 41; seq <= 46; seq++) r.push(seq, 0, r.tickFor(seq), null);
  r.reconcile(0, 43, 102, null); // d = 59: the server dropped one step; pending 44..46
  assert.equal(r.state(), 4); // 1 dropped step (input 0) + 3 pending
});
