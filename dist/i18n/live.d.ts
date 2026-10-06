/** Remembers how to redraw node in the current language; returns node. apply must not capture node (weak). */
export declare function track<N extends object>(node: N, apply: (n: N) => void): N;
/** Redraws every live label (setLang calls it). */
export declare function relabel(): void;
/** Live entries (tests). */
export declare function liveCount(): number;
