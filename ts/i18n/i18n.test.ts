import { test } from "node:test";
import assert from "node:assert/strict";
import { createI18n } from "./i18n.ts";
import type { Msg } from "./types.ts";

type L = "aa" | "bb";
type K = "a" | "p" | "only";
const make = () =>
  createI18n<L, K>({
    dicts: {
      aa: { a: "A {n}", p: { one: "{n} item", other: "{n} items" }, only: "base" },
      bb: { a: "B {n}", p: { one: "{n} thing", other: "{n} things" } } as unknown as Record<K, Msg>,
    },
    source: "aa", langs: ["aa", "bb"], storeKey: "test.lang",
    detect: (prefs) => (prefs[0] === "bb" ? "bb" : "aa"),
  });
const mem = () => {
  const m = new Map<string, string>();
  return { m, getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
};

test("starts in the source language; missing keys fall back to it; params and plurals", () => {
  const i = make();
  assert.equal(i.lang(), "aa");
  assert.equal(i.t("a", { n: 3 }), "A 3");
  assert.equal(i.t("a"), "A {n}");
  assert.equal(i.t("p", { n: 1 }), "1 item");
  assert.equal(i.t("p", { n: 2 }), "2 items");
  i.setLang("bb", null);
  assert.equal(i.t("a", { n: 3 }), "B 3");
  assert.equal(i.t("p", { n: 1 }), "1 thing");
  assert.equal(i.t("only"), "base");
  assert.equal(i.tIn("aa", "a"), "A {n}");
  assert.equal(i.tIn("aa", "p"), "{n} items");
});

test("setLang stores under storeKey and notifies listeners once; an unchanged language is silent", () => {
  const i = make();
  const s = mem();
  let calls = 0;
  const off = i.onLang(() => calls++);
  i.setLang("bb", s);
  assert.equal(s.m.get("test.lang"), "bb");
  assert.equal(calls, 1);
  i.setLang("bb", s);
  assert.equal(calls, 1);
  off();
  i.setLang("aa", s);
  assert.equal(calls, 1);
});

test("initialLang prefers the stored value, else detect; blocked storage does not throw", () => {
  const i = make();
  const s = mem();
  assert.equal(i.initialLang(["bb"], s), "bb");
  s.m.set("test.lang", "aa");
  assert.equal(i.initialLang(["bb"], s), "aa");
  s.m.set("test.lang", "zz");
  assert.equal(i.initialLang(["bb"], s), "bb");
  const blocked = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
  assert.equal(i.initialLang(["bb"], blocked), "bb");
  i.setLang("bb", blocked);
  assert.equal(i.lang(), "bb");
  assert.ok(i.isLang("aa") && !i.isLang("zz") && !i.isLang(null));
  assert.equal(i.detectLang(["bb"]), "bb");
});

test("destructured functions work", () => {
  const { t, setLang, lang } = make();
  setLang("bb", null);
  assert.equal(lang(), "bb");
  assert.equal(t("a", { n: 1 }), "B 1");
});

test("makeFormat follows the language", async () => {
  const { makeFormat } = await import("./format.ts");
  let l = "tr";
  const f = makeFormat(() => l === "tr");
  assert.equal(f.num(0.55, 2), "0,55");
  assert.equal(f.fixed(1.25, 1), "1,3");
  l = "en";
  assert.equal(f.num(0.55, 2), "0.55");
  assert.equal(f.num(2), "2");
});
