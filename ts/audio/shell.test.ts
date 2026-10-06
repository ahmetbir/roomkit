import { test } from "node:test";
import assert from "node:assert/strict";
import { AudioShell, falloff } from "./shell.ts";

const fakeTarget = () => {
  const l = new Map<string, Set<() => void>>();
  return {
    l,
    addEventListener: (t: string, f: () => void) => void (l.get(t) ?? l.set(t, new Set()).get(t)!).add(f),
    removeEventListener: (t: string, f: () => void) => void l.get(t)?.delete(f),
  };
};
const count = (x: { l: Map<string, Set<() => void>> }) => [...x.l.values()].reduce((n, s) => n + s.size, 0);

test("before and without a context every call is a no-op; a gesture without Web Audio stays silent", () => {
  const win = fakeTarget();
  const doc = { ...fakeTarget(), hidden: false };
  let started = 0;
  const a = new AudioShell({ start: () => void started++ }, win as never, doc as never);
  assert.equal(a.live(), false);
  assert.equal(a.context(), null);
  a.setVolume(0.5);
  a.burst({ type: "lowpass", f0: 500, q: 1 }, 0.1, 1);
  a.tone("sine", 400, 800, 0.1, 1);
  for (const f of win.l.get("pointerdown") ?? []) f(); // no AudioContext in Node: stays silent
  for (const f of doc.l.get("visibilitychange") ?? []) f();
  assert.equal(a.live(), false);
  assert.equal(a.context(), null);
  assert.equal(started, 0);
  a.dispose();
});

test("dispose removes exactly the three listeners it added", () => {
  const win = fakeTarget();
  const doc = { ...fakeTarget(), hidden: false };
  const a = new AudioShell({ start: () => {} }, win as never, doc as never);
  assert.deepEqual([count(win), count(doc)], [2, 1]);
  assert.deepEqual([...win.l.keys()].sort(), ["keydown", "pointerdown"]);
  a.dispose();
  assert.deepEqual([count(win), count(doc)], [0, 0]);
});

test("falloff: full volume at the source, half at 500 m", () => {
  assert.equal(falloff(0), 1);
  assert.equal(falloff(-5), 1);
  assert.equal(falloff(500), 0.5);
});

// A minimal Web Audio double that records what the shell builds.
function fakeAudio() {
  const log = { created: 0, sources: 0, targets: [] as number[] };
  const param = (v = 0) => ({
    value: v,
    setValueAtTime() {},
    exponentialRampToValueAtTime() {},
    setTargetAtTime: (x: number) => void log.targets.push(x),
  });
  const node = (): Record<string, unknown> => {
    const n: Record<string, unknown> = {
      gain: param(1), frequency: param(), Q: param(), playbackRate: param(1),
      threshold: param(), knee: param(), ratio: param(), attack: param(), release: param(),
      connect: () => n, start() {}, stop() {},
    };
    return n;
  };
  class Ctx {
    state = "running";
    currentTime = 0;
    sampleRate = 8;
    destination = {};
    constructor() { log.created++; }
    createGain = node;
    createDynamicsCompressor = node;
    createBiquadFilter = node;
    createOscillator = node;
    createBufferSource = () => (log.sources++, node());
    createBuffer = (_c: number, len: number) => ({ duration: 2, getChannelData: () => new Float32Array(len) });
    suspend = async () => {};
    resume = async () => {};
    close = async () => {};
  }
  return { log, Ctx };
}

test("a gesture starts the audio once; the volume set before it is honoured and mute silences one-shots", () => {
  const { log, Ctx } = fakeAudio();
  const g = globalThis as { AudioContext?: unknown };
  const prev = g.AudioContext;
  g.AudioContext = Ctx;
  try {
    const win = fakeTarget();
    const doc = { ...fakeTarget(), hidden: false };
    let master: { gain: { value: number } } | undefined;
    let started = 0;
    const a = new AudioShell({ start: (_c, m) => { started++; master = m as never; } }, win as never, doc as never);
    a.setVolume(0.4);
    a.burst({ type: "lowpass", f0: 500, q: 1 }, 0.1, 1);
    assert.equal(log.created, 0, "nothing before a gesture");
    for (const f of win.l.get("pointerdown") ?? []) f();
    for (const f of win.l.get("keydown") ?? []) f();
    assert.equal(log.created, 1, "one context for any number of gestures");
    assert.equal(started, 1);
    assert.equal(a.live(), true);
    assert.equal(master?.gain.value, 0.4);
    a.burst({ type: "lowpass", f0: 500, q: 1 }, 0.1, 1);
    a.tone("sine", 400, 800, 0.1, 1);
    assert.equal(log.sources, 1, "the burst plays");
    a.setVolume(0);
    assert.deepEqual(log.targets, [0], "mute ramps the master gain to 0");
    a.burst({ type: "lowpass", f0: 500, q: 1 }, 0.1, 0.001); // below the audible floor
    assert.equal(log.sources, 1);
    a.dispose();
  } finally {
    g.AudioContext = prev;
  }
});
