export type Store = Pick<Storage, "getItem" | "setItem">;
export declare function safeStore(): Store | null;
export type Slot = {
    readonly key: string;
    get(store?: Pick<Storage, "getItem"> | null): string | null;
    set(v: string, store?: Pick<Storage, "setItem"> | null): void;
};
/** One stored string under its full key (games prefix their keys: "dogfight.lang"). */
export declare function slot(key: string): Slot;
