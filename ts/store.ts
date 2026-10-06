// localStorage access that never throws: blocked storage (privacy modes)
// reads as empty and drops writes.
export type Store = Pick<Storage, "getItem" | "setItem">;

export function safeStore(): Store | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null; // blocked storage throws on access
  }
}

export type Slot = {
  readonly key: string;
  get(store?: Pick<Storage, "getItem"> | null): string | null;
  set(v: string, store?: Pick<Storage, "setItem"> | null): void;
};

/** One stored string under its full key (games prefix their keys: "dogfight.lang"). */
export function slot(key: string): Slot {
  return {
    key,
    get(store = safeStore()) {
      try {
        return store?.getItem(key) ?? null;
      } catch {
        return null;
      }
    },
    set(v, store = safeStore()) {
      try {
        store?.setItem(key, v);
      } catch {
        // storage blocked: the value lives for this page only
      }
    },
  };
}
