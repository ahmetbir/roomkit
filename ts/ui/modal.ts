// Keyboard rules of a modal with a tab list.

/** The tab index a key moves to from i among n tabs; null for other keys. */
export function tabMove(key: string, i: number, n: number): number | null {
  switch (key) {
    case "ArrowDown": case "ArrowRight": return (i + 1) % n;
    case "ArrowUp": case "ArrowLeft": return (i - 1 + n) % n;
    case "Home": return 0;
    case "End": return n - 1;
  }
  return null;
}

/**
 * Focus trap step: the index Tab (or Shift+Tab) moves to among n focusable
 * elements from cur (-1 = focus is outside the modal: enter at the first, or
 * the last with Shift).
 */
export function trapIndex(cur: number, n: number, shift: boolean): number {
  if (cur < 0) return shift ? n - 1 : 0;
  return shift ? (cur - 1 + n) % n : (cur + 1) % n;
}
