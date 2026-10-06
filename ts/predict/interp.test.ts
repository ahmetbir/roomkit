import { test } from "node:test";
import assert from "node:assert/strict";
import { extrapolate, InterpBuffer, ServerClock } from "./interp.ts";

const lerp = (a: number, b: number, u: number) => a + (b - a) * u;
const buf = () => new InterpBuffer<number>(lerp);

test("sample mixes neighbours, clamps at both ends", () => {
  const b = buf();
  assert.equal(b.sample(0), null);
  b.push(0, 0);
  b.push(100, 10);
  assert.equal(b.sample(50), 5);
  assert.equal(b.sample(-50), 0);
  assert.equal(b.sample(500), 10);
});

test("out-of-order pushes are ignored and old entries trimmed", () => {
  const b = buf();
  b.push(100, 1);
  b.push(100, 2);
  b.push(50, 3);
  assert.equal(b.size(), 1);
  assert.equal(b.newest()!.fs, 1);
  for (let i = 0; i < 100; i++) b.push(200 + i, i);
  assert.ok(b.size() <= 32);
});

test("extrapolate interpolates inside, carries ahead past the end, capped at 500 ms", () => {
  const b = buf();
  b.push(0, 0);
  b.push(100, 10);
  const ahead = (s: number, sec: number) => s + 10 * sec;
  assert.equal(extrapolate(b, 50, ahead), 5);
  assert.equal(extrapolate(b, 200, ahead), 11);
  assert.equal(extrapolate(b, 5000, ahead), 15);
  assert.equal(extrapolate(buf(), 0, ahead), null);
});

test("ServerClock maps ticks with its own tick length and renders behind by its delay", () => {
  const c = new ServerClock(50, 0);
  assert.equal(c.serverMs(3), 150);
  c.observe(3, 1150); // delay 1000 ms
  assert.equal(c.renderTime(1250), 250);
  const d = new ServerClock(50, 20);
  d.observe(0, 100);
  assert.equal(d.renderTime(100), -20);
  assert.ok(Math.abs(new ServerClock().serverMs(60) - 1000) < 1e-9);
});

test("the offset follows the lowest delay and relaxes 5 ms per 5 s", () => {
  const c = new ServerClock(50, 0);
  c.observe(0, 1000);
  c.observe(1, 1010); // delay 960 < 1000
  assert.equal(c.renderTime(2000), 2000 - 960);
  c.observe(2, 6100); // delay 6000: +5 relax once, then min keeps 965
  assert.equal(c.renderTime(2000), 2000 - 965);
});
