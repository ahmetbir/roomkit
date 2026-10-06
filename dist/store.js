export function safeStore() {
    try {
        return globalThis.localStorage ?? null;
    }
    catch {
        return null; // blocked storage throws on access
    }
}
/** One stored string under its full key (games prefix their keys: "dogfight.lang"). */
export function slot(key) {
    return {
        key,
        get(store = safeStore()) {
            try {
                return store?.getItem(key) ?? null;
            }
            catch {
                return null;
            }
        },
        set(v, store = safeStore()) {
            try {
                store?.setItem(key, v);
            }
            catch {
                // storage blocked: the value lives for this page only
            }
        },
    };
}
