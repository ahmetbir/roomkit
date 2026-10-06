// Room codes: 4 characters, no look-alikes (0/O, 1/I).
const CODE_RE = /^[A-HJ-NP-Z2-9]{4}$/;

/** Normalized room code, or "" when it cannot be one. */
export function normalizeCode(s: string): string {
  const c = s.trim().toUpperCase();
  return CODE_RE.test(c) ? c : "";
}
