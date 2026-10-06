// Labels that follow a language switch. Only weak references are kept, so a
// label that left the page is collected; dead entries are pruned as we go.
/** The page's live labels; track() and relabel() are its only API. */
const reg = { entries: [], added: 0 };
const PRUNE_EVERY = 256;
function prune() {
    let j = 0;
    for (const e of reg.entries)
        if (e.ref.deref() !== undefined)
            reg.entries[j++] = e;
    reg.entries.length = j;
}
/** Remembers how to redraw node in the current language; returns node. apply must not capture node (weak). */
export function track(node, apply) {
    reg.entries.push({ ref: new WeakRef(node), apply: apply });
    if (++reg.added % PRUNE_EVERY === 0)
        prune();
    return node;
}
/** Redraws every live label (setLang calls it). */
export function relabel() {
    prune();
    for (const e of reg.entries) {
        const n = e.ref.deref();
        if (n !== undefined)
            e.apply(n);
    }
}
/** Live entries (tests). */
export function liveCount() {
    prune();
    return reg.entries.length;
}
