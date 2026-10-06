// The pilot token: issued by the server in welcome, kept in localStorage,
// sent back in hello and in the X-Pilot-Token header only (never in URLs).
import type { Slot } from "../store.ts";

export const TOKEN_RE = /^[A-Za-z0-9_-]{22}$/;

export function tokenStore(s: Slot) {
  return {
    /** The stored token, "" when absent or invalid. */
    load(store?: Pick<Storage, "getItem"> | null): string {
      const t = s.get(store) ?? "";
      return TOKEN_RE.test(t) ? t : "";
    },
    /** Stores tok; invalid tokens are ignored. */
    save(tok: string, store?: Pick<Storage, "setItem"> | null): void {
      if (TOKEN_RE.test(tok)) s.set(tok, store);
    },
  };
}
