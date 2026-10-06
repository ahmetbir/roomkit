import { test } from "node:test";
import assert from "node:assert/strict";
import { slot } from "./store.ts";

test("slot reads and writes its exact key and survives blocked storage", () => {
  const m = new Map<string, string>();
  const st = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
  const s = slot("game.x");
  s.set("1", st);
  assert.equal(m.get("game.x"), "1");
  assert.equal(s.get(st), "1");
  const blocked = { getItem: () => { throw new Error("no"); }, setItem: () => { throw new Error("no"); } };
  assert.equal(s.get(blocked), null);
  s.set("2", blocked); // no throw
});
