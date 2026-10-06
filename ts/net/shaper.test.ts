import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_BUFFERED, STALL_MS, Shaper, type ShaperPolicy } from "./shaper.ts";

type In = { t: "in"; seq: number; shot?: boolean };
type M = In | { t: "pick"; k: string } | { t: "chat"; id: number } | { t: "ping"; ts: number };

const PICK_GAP_MS = 500;
const policy: ShaperPolicy<M> = {
  isInput: (m) => m.t === "in",
  latch: (held, m) => (m.t === "in" && held.t === "in" ? { ...m, shot: !!(m.shot || held.shot) } : m),
  gapped: (m) => m.t === "pick",
  gapMs: PICK_GAP_MS,
};

function rig() {
  let t = 0;
  let buffered = 0;
  const sent: M[] = [];
  const timers: { at: number; f: () => void; live: boolean }[] = [];
  const s = new Shaper<M>({
    now: () => t,
    setTimeout: (f, ms) => { const h = { at: t + ms, f, live: true }; timers.push(h); return h; },
    clearTimeout: (h) => { (h as { live: boolean }).live = false; },
    buffered: () => buffered,
    write: (m) => { sent.push(m); return true; },
  }, policy);
  const advance = (ms: number) => {
    t += ms;
    for (const h of timers) if (h.live && h.at <= t) { h.live = false; h.f(); }
  };
  return { s, sent, advance, setBuffered: (n: number) => { buffered = n; } };
}

const input = (seq: number, extra: Partial<In> = {}): In => ({ t: "in", seq, ...extra });

test("a 3 s network stall sends one input when traffic resumes, not 180", () => {
  const r = rig();
  r.s.send(input(1));
  for (let seq = 2; seq <= 181; seq++) { // 3 s at 60 Hz, no snap arriving
    r.advance(1000 / 60);
    assert.equal(r.s.send(input(seq, { shot: seq === 100 })), true, "accepted: the game predicts it");
  }
  const during = r.sent.length;
  assert.ok(during <= 1 + 60, `sent ${during} inputs into the stall`); // at most STALL_MS worth
  r.s.received(); // the network is back
  const after = r.sent.slice(during) as In[];
  assert.equal(after.length, 1);
  assert.equal(after[0].seq, 181, "only the newest input");
  assert.equal(after[0].shot, true, "a one-shot press inside the stall is not lost");
  r.s.send(input(182));
  assert.equal((r.sent.at(-1) as In).seq, 182, "live again");
  assert.equal(!!(r.sent.at(-1) as In).shot, false);
});

test("a full browser send buffer holds inputs too", () => {
  const r = rig();
  r.s.received();
  r.setBuffered(MAX_BUFFERED + 1);
  r.s.send(input(1));
  r.s.send(input(2, { shot: true }));
  assert.equal(r.sent.length, 0);
  r.setBuffered(0);
  r.s.send(input(3));
  assert.deepEqual(r.sent.map((m) => (m as In).seq), [3]);
  assert.equal((r.sent[0] as In).shot, true);
});

test("steady traffic passes every input through", () => {
  const r = rig();
  for (let seq = 1; seq <= 120; seq++) {
    r.advance(1000 / 60);
    if (seq % 2 === 0) r.s.received(); // snaps at 30 Hz
    r.s.send(input(seq));
  }
  assert.equal(r.sent.length, 120);
  assert.ok(STALL_MS > 100);
});

test("picks: at most one per PICK_GAP_MS, the newest of a burst goes out", () => {
  const r = rig();
  r.s.send({ t: "pick", k: "f16" });
  assert.equal(r.s.send({ t: "pick", k: "mig29" }), true);
  r.s.send({ t: "pick", k: "su27" });
  assert.equal(r.sent.length, 1);
  r.advance(PICK_GAP_MS - 1);
  assert.equal(r.sent.length, 1);
  r.advance(1);
  assert.deepEqual(r.sent.map((m) => (m as { k: string }).k), ["f16", "su27"]);
  for (let i = 0; i < 20; i++) { r.s.send({ t: "pick", k: "f16" }); r.advance(50); } // 1 s of spam
  assert.ok(r.sent.length <= 2 + 3, `${r.sent.length} picks`);
});

test("dispose drops a held pick and input", () => {
  const r = rig();
  r.s.send({ t: "pick", k: "f16" });
  r.s.send({ t: "pick", k: "su27" });
  r.s.dispose();
  r.advance(PICK_GAP_MS);
  assert.equal(r.sent.length, 1);
});

test("other messages pass untouched", () => {
  const r = rig();
  r.advance(STALL_MS * 3);
  r.s.send({ t: "ping", ts: 1 });
  r.s.send({ t: "chat", id: 2 });
  assert.equal(r.sent.length, 2);
});
