// Labels that follow a language switch. Only weak references are kept, so a
// label that left the page is collected; dead entries are pruned as we go.

type Entry = { ref: WeakRef<object>; apply: (n: never) => void };

/** The page's live labels; track() and relabel() are its only API. */
const reg: { entries: Entry[]; added: number } = { entries: [], added: 0 };
const PRUNE_EVERY = 256;

function prune(): void {
  let j = 0;
  for (const e of reg.entries) if (e.ref.deref() !== undefined) reg.entries[j++] = e;
  reg.entries.length = j;
}

/** Remembers how to redraw node in the current language; returns node. apply must not capture node (weak). */
export function track<N extends object>(node: N, apply: (n: N) => void): N {
  reg.entries.push({ ref: new WeakRef(node), apply: apply as (n: never) => void });
  if (++reg.added % PRUNE_EVERY === 0) prune();
  return node;
}

/** Redraws every live label (setLang calls it). */
export function relabel(): void {
  prune();
  for (const e of reg.entries) {
    const n = e.ref.deref();
    if (n !== undefined) (e.apply as (n: object) => void)(n);
  }
}

/** Live entries (tests). */
export function liveCount(): number {
  prune();
  return reg.entries.length;
}
