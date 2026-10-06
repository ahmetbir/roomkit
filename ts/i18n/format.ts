// Number formatting in the current language: a decimal comma or a point.

/** num/fixed; `decimalComma()` says whether the current language writes a decimal comma. */
export function makeFormat(decimalComma: () => boolean): {
  num(v: number, digits?: number): string;
  fixed(v: number, digits: number): string;
} {
  const sep = (s: string) => (decimalComma() ? s.replace(".", ",") : s);
  return {
    /** Rounded with at most `digits` decimals: 0.55 → "0,55" (tr) / "0.55" (en). */
    num: (v, digits = 1) => sep(String(Math.round(v * 10 ** digits) / 10 ** digits)),
    /** Exactly `digits` decimals: 1.25 → "1,3" / "1.3". */
    fixed: (v, digits) => sep(v.toFixed(digits)),
  };
}
