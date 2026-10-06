// Tiny DOM helper (no framework). Text is only ever set through text nodes,
// never innerHTML, so user-controlled strings cannot inject markup (S8).

export type Attrs = Record<string, string | number | boolean | null | undefined>;
export type Child = Node | string | number | null | undefined | false;

/**
 * h("div", { class: "panel" }, "text", child) builds an element. Attribute
 * names starting with "on" and "style" are refused: handlers are attached with
 * addEventListener, styles come from style.css or individual style properties.
 */
export function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs: Attrs = {}, ...children: Child[]): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (/^on/i.test(k) || k.toLowerCase() === "style") throw new Error(`h: attribute ${k} not allowed`);
    if (v === false || v === null || v === undefined) continue;
    e.setAttribute(k, v === true ? "" : String(v));
  }
  append(e, ...children);
  return e;
}

export function append(e: Node, ...children: Child[]): void {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    e.appendChild(typeof c === "string" || typeof c === "number" ? document.createTextNode(String(c)) : c);
  }
}

/** Replaces e's children. */
export function fill(e: Node, ...children: Child[]): void {
  e.textContent = "";
  append(e, ...children);
}

/** Sets text only when it changed (cheap at 10 Hz). */
export function text(e: Node, s: string): void {
  if (e.textContent !== s) e.textContent = s;
}

/** "m:ss" for whole seconds. */
export function clock(s: number): string {
  const t = Math.max(0, Math.ceil(s));
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, "0")}`;
}
