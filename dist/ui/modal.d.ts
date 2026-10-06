/** The tab index a key moves to from i among n tabs; null for other keys. */
export declare function tabMove(key: string, i: number, n: number): number | null;
/**
 * Focus trap step: the index Tab (or Shift+Tab) moves to among n focusable
 * elements from cur (-1 = focus is outside the modal: enter at the first, or
 * the last with Shift).
 */
export declare function trapIndex(cur: number, n: number, shift: boolean): number;
