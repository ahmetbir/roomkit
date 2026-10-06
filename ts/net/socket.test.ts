import { test } from "node:test";
import assert from "node:assert/strict";
import { RECOVERABLE, ROOM_GONE } from "./codes.ts";
import type { ShaperPolicy } from "./shaper.ts";
import { Socket, socketURL, PING_MS, FIRST_TRIES, type Conn, type Env, type Status } from "./socket.ts";

const VERSION = 7;
type M = { t: "in"; seq: number; shot?: boolean } | { t: "pick"; k: string } | { t: "create"; seats?: number; mode?: string };
type S = { t: string; code?: string; msg?: string };
const policy: ShaperPolicy<M> = {
  isInput: (m) => m.t === "in",
  latch: (held, m) => (m.t === "in" && held.t === "in" ? { ...m, shot: !!(m.shot || held.shot) } : m),
  gapped: (m) => m.t === "pick",
  gapMs: 500,
};

class FakeConn implements Conn {
  readyState = 0;
  sent: any[] = [];
  closed = false;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  send(data: string) { this.sent.push(JSON.parse(data)); }
  close() { this.closed = true; this.readyState = 3; }
  open() { this.readyState = 1; this.onopen?.({} as Event); }
  recv(m: object) { this.onmessage?.({ data: JSON.stringify(m) } as MessageEvent); }
  drop(code = 1006) { this.readyState = 3; this.onclose?.({ code } as CloseEvent); }
}

type Timer = { at: number; f: () => void; every: number; live: boolean };

function fakeEnv() {
  let t = 0;
  const timers: Timer[] = [];
  const conns: FakeConn[] = [];
  const env: Env = {
    dial: () => { const c = new FakeConn(); conns.push(c); return c; },
    now: () => t,
    setTimeout: (f, ms) => { const h = { at: t + ms, f, every: 0, live: true }; timers.push(h); return h; },
    clearTimeout: (h) => { (h as Timer).live = false; },
    setInterval: (f, ms) => { const h = { at: t + ms, f, every: ms, live: true }; timers.push(h); return h; },
    clearInterval: (h) => { (h as Timer).live = false; },
  };
  const advance = (ms: number) => {
    const end = t + ms;
    for (;;) {
      const due = timers.filter((x) => x.live && x.at <= end).sort((a, b) => a.at - b.at)[0];
      if (!due) break;
      t = due.at;
      if (due.every > 0) due.at += due.every; else due.live = false;
      due.f();
    }
    t = end;
  };
  return { env, conns, advance };
}

function harness(entry: any = { t: "create", mode: "ffa", seats: 4 }) {
  const f = fakeEnv();
  const msgs: S[] = [];
  const status: Status[] = [];
  const fatal: string[] = [];
  let unreachable = 0;
  const s = new Socket<M, S>("ws://x/ws", "Ace", entry, {
    onMsg: (m) => msgs.push(m), onStatus: (st) => status.push(st), onFatal: (code, m) => fatal.push(`${code}|${m}`),
    onUnreachable: () => unreachable++,
  }, { version: VERSION, policy }, f.env);
  return { ...f, s, msgs, status, fatal, unreachable: () => unreachable };
}

const welcome = (code: string) => ({ t: "welcome", you: 1, code });

test("socketURL picks ws/wss from the page scheme", () => {
  assert.equal(socketURL({ protocol: "http:", host: "localhost:8080" }), "ws://localhost:8080/ws");
  assert.equal(socketURL({ protocol: "https:", host: "a.dev" }), "wss://a.dev/ws");
});

test("handshake sends hello + create, then joins by code on reconnect", () => {
  const h = harness();
  h.conns[0].open();
  assert.deepEqual(h.conns[0].sent, [
    { t: "hello", v: 7, name: "Ace" },
    { t: "create", mode: "ffa", seats: 4 },
  ]);
  h.conns[0].recv(welcome("ABCD"));
  assert.equal(h.s.code(), "ABCD");
  assert.deepEqual(h.status, ["connecting", "open"]);

  h.conns[0].drop();
  assert.equal(h.conns.length, 1);
  h.advance(499);
  assert.equal(h.conns.length, 1);
  h.advance(1);
  assert.equal(h.conns.length, 2);
  h.conns[1].open();
  assert.deepEqual(h.conns[1].sent, [{ t: "hello", v: 7, name: "Ace" }, { t: "join", code: "ABCD" }]);
  assert.deepEqual(h.status, ["connecting", "open", "connecting"]);
});

test("quick play sends hello + quick, then rejoins its room by code", () => {
  const h = harness({ t: "quick" });
  h.conns[0].open();
  assert.deepEqual(h.conns[0].sent, [{ t: "hello", v: VERSION, name: "Ace" }, { t: "quick" }]);
  h.conns[0].recv(welcome("QK12"));
  h.conns[0].drop();
  h.advance(500);
  h.conns[1].open();
  assert.deepEqual(h.conns[1].sent, [{ t: "hello", v: VERSION, name: "Ace" }, { t: "join", code: "QK12" }]);
});

test("send drops messages until the welcome arrives", () => {
  const h = harness({ t: "join", code: "WXYZ" });
  h.s.send({ t: "pick", k: "f16" });
  h.conns[0].open();
  h.s.send({ t: "pick", k: "f16" });
  assert.equal(h.conns[0].sent.length, 2);
  h.conns[0].recv(welcome("WXYZ"));
  h.s.send({ t: "pick", k: "f16" });
  assert.deepEqual(h.conns[0].sent[2], { t: "pick", k: "f16" });
  assert.equal(h.msgs[0].t, "welcome");
});

test("reconnect delays back off 0.5,1,2,4,8,8 s", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD")); // once in a room, reconnecting never gives up
  const delays: number[] = [];
  for (let i = 0; i < 6; i++) {
    const n = h.conns.length;
    h.conns[n - 1].drop();
    let ms = 0;
    while (h.conns.length === n) { h.advance(100); ms += 100; }
    delays.push(ms);
  }
  assert.deepEqual(delays, [500, 1000, 2000, 4000, 8000, 8000]);
});

test("pings every 15 s from open, also before welcome", () => {
  const h = harness();
  h.conns[0].open();
  h.advance(PING_MS);
  assert.equal(h.conns[0].sent[2].t, "ping");
  h.conns[0].recv(welcome("ABCD"));
  h.advance(PING_MS);
  assert.equal(h.conns[0].sent.filter((m) => m.t === "ping").length, 2);
});

test("server error is fatal: no reconnect, reported once", () => {
  const h = harness({ t: "join", code: "NOPE" });
  h.conns[0].open();
  h.conns[0].recv({ t: "error", code: "no_room", msg: "room text" });
  h.conns[0].drop();
  h.advance(60000);
  assert.equal(h.conns.length, 1);
  assert.deepEqual(h.fatal, ["no_room|room text"]);
  assert.equal(h.status.at(-1), "closed");
  assert.equal(h.msgs.length, 0);
});

test("close stops reconnecting", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].drop();
  h.s.close();
  h.advance(60000);
  assert.equal(h.conns.length, 1);
  assert.equal(h.status.at(-1), "closed");
});

test("send reports whether it wrote; nothing is queued while reconnecting", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  const inp = { t: "in", seq: 1 } as const;
  assert.equal(h.s.send(inp), true);
  h.conns[0].drop();
  for (let i = 0; i < 100; i++) assert.equal(h.s.send({ ...inp, seq: i + 2 }), false);
  h.advance(500);
  h.conns[1].open();
  h.conns[1].recv(welcome("ABCD"));
  assert.deepEqual(h.conns[1].sent.map((m) => m.t), ["hello", "join"], "dropped inputs never flush");
});

test("a throwing dial schedules the next reconnect", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  const dial = h.env.dial;
  let fails = 2;
  h.env.dial = (url) => { if (fails-- > 0) throw new Error("SecurityError"); return dial(url); };
  h.conns[0].drop();
  h.advance(500);  // first retry throws
  h.advance(1000); // second retry throws
  assert.equal(h.conns.length, 1);
  h.advance(2000); // third retry dials
  assert.equal(h.conns.length, 2);
  assert.equal(h.status.at(-1), "connecting");
});

test("non-object JSON is ignored", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].onmessage?.({ data: "null" } as MessageEvent);
  h.conns[0].onmessage?.({ data: "42" } as MessageEvent);
  assert.equal(h.msgs.length, 0);
});

test("the first connection gives up after FIRST_TRIES dials (~15 s) and can be retried", () => {
  const h = harness();
  for (let i = 0; i < FIRST_TRIES; i++) {
    assert.equal(h.conns.length, i + 1);
    assert.equal(h.unreachable(), 0);
    h.conns[i].drop(); // server down: every dial fails before a welcome
    h.advance(8000);
  }
  assert.equal(h.unreachable(), 1);
  assert.equal(h.status.at(-1), "closed");
  h.advance(60000);
  assert.equal(h.conns.length, FIRST_TRIES, "no dial after giving up");
  assert.equal(h.fatal.length, 0);

  h.s.retry();
  assert.equal(h.conns.length, FIRST_TRIES + 1);
  assert.equal(h.status.at(-1), "connecting");
  h.conns.at(-1)!.open();
  h.conns.at(-1)!.recv(welcome("ABCD"));
  assert.equal(h.status.at(-1), "open");
});

test("hello carries the pilot token once set; invalid tokens are ignored", () => {
  const h = harness();
  h.s.setToken("not a token");
  h.s.setToken("AAAAAAAAAAAAAAAAAAAAAA");
  h.conns[0].open();
  assert.deepEqual(h.conns[0].sent[0], { t: "hello", v: VERSION, name: "Ace", tok: "AAAAAAAAAAAAAAAAAAAAAA" });
});

test("a flood error in game reconnects to the room instead of ending it", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  h.conns[0].recv({ t: "error", code: [...RECOVERABLE][0], msg: "x" });
  assert.equal(h.fatal.length, 0);
  assert.equal(h.conns[0].closed, true);
  assert.equal(h.status.at(-1), "connecting", "the reconnect banner");
  h.advance(500);
  h.conns[1].open();
  assert.deepEqual(h.conns[1].sent[1], { t: "join", code: "ABCD" });
});

test("in game: picks keep a 500 ms gap, inputs during a stall reach the server as one", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  const c = h.conns[0];
  h.s.send({ t: "pick", k: "f16" });
  h.s.send({ t: "pick", k: "su27" });
  assert.equal(c.sent.filter((m) => m.t === "pick").length, 1);
  h.advance(500);
  assert.deepEqual(c.sent.filter((m) => m.t === "pick").map((m) => m.k), ["f16", "su27"]);
  const inp = { t: "in" } as const;
  
  const before = c.sent.length;
  for (let seq = 1; seq <= 180; seq++) { h.advance(1000 / 60); h.s.send({ ...inp, seq }); } // 3 s, no snaps
  c.recv({ t: "pong", ts: 0 });
  const ins = c.sent.slice(before).filter((m) => m.t === "in");
  assert.ok(ins.length <= 62, `${ins.length} inputs`);
  assert.equal(ins.at(-1).seq, 180);
});

test("close 1012 (server updating) reports 'updating' and rejoins the room on the new server", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  h.conns[0].drop(1012);
  assert.deepEqual(h.status, ["connecting", "open", "updating"]);
  h.advance(500);
  assert.equal(h.conns.length, 2);
  h.conns[1].open();
  assert.deepEqual(h.conns[1].sent, [{ t: "hello", v: VERSION, name: "Ace" }, { t: "join", code: "ABCD" }]);
  h.conns[1].recv(welcome("ABCD")); // the room exists on the new server
  assert.equal(h.status.at(-1), "open");
  assert.equal(h.fatal.length, 0);
});

test("after an update the old room is gone: a friendly fatal instead of no_room", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  h.conns[0].drop(1012);
  h.advance(500);
  h.conns[1].open();
  h.conns[1].recv({ t: "error", code: "no_room", msg: "room text" });
  assert.deepEqual(h.fatal, [`${ROOM_GONE}|room text`]);
  // without an update the server's own text stays
  const g = harness({ t: "join", code: "NOPE" });
  g.conns[0].open();
  g.conns[0].recv({ t: "error", code: "no_room", msg: "room text" });
  assert.deepEqual(g.fatal, ["no_room|room text"]);
});

test("a 1012 before the first welcome retries the same entry at once", () => {
  const h = harness({ t: "quick" });
  h.conns[0].open();
  h.conns[0].drop(1012); // a draining server refused the handshake
  assert.equal(h.status.at(-1), "updating");
  h.advance(500);
  h.conns[1].open();
  assert.deepEqual(h.conns[1].sent[1], { t: "quick" });
  assert.equal(h.unreachable(), 0);
});

test("1012s in a row back off instead of redialing every 500 ms", () => {
  const h = harness();
  h.conns[0].open();
  h.conns[0].recv(welcome("ABCD"));
  const delays: number[] = [];
  for (let i = 0; i < 4; i++) {
    const n = h.conns.length;
    h.conns[n - 1].drop(1012);
    let ms = 0;
    while (h.conns.length === n) { h.advance(100); ms += 100; }
    delays.push(ms);
  }
  assert.deepEqual(delays, [500, 500, 1000, 2000]);
  assert.equal(h.fatal.length, 0);
});
