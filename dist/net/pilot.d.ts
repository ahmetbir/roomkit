import type { Slot } from "../store.js";
export declare const TOKEN_RE: RegExp;
export declare function tokenStore(s: Slot): {
    /** The stored token, "" when absent or invalid. */
    load(store?: Pick<Storage, "getItem"> | null): string;
    /** Stores tok; invalid tokens are ignored. */
    save(tok: string, store?: Pick<Storage, "setItem"> | null): void;
};
