export const TOKEN_RE = /^[A-Za-z0-9_-]{22}$/;
export function tokenStore(s) {
    return {
        /** The stored token, "" when absent or invalid. */
        load(store) {
            const t = s.get(store) ?? "";
            return TOKEN_RE.test(t) ? t : "";
        },
        /** Stores tok; invalid tokens are ignored. */
        save(tok, store) {
            if (TOKEN_RE.test(tok))
                s.set(tok, store);
        },
    };
}
