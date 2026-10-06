export type Attrs = Record<string, string | number | boolean | null | undefined>;
export type Child = Node | string | number | null | undefined | false;
/**
 * h("div", { class: "panel" }, "text", child) builds an element. Attribute
 * names starting with "on" and "style" are refused: handlers are attached with
 * addEventListener, styles come from style.css or individual style properties.
 */
export declare function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs?: Attrs, ...children: Child[]): HTMLElementTagNameMap[K];
export declare function append(e: Node, ...children: Child[]): void;
/** Replaces e's children. */
export declare function fill(e: Node, ...children: Child[]): void;
/** Sets text only when it changed (cheap at 10 Hz). */
export declare function text(e: Node, s: string): void;
/** "m:ss" for whole seconds. */
export declare function clock(s: number): string;
