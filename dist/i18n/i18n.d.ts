import { type Store } from "../store.js";
import type { Msg, Params } from "./types.js";
export type I18nOptions<L extends string, K extends string> = {
    dicts: Record<L, Readonly<Record<K, Msg>>>;
    source: L;
    langs: readonly L[];
    storeKey: string;
    detect(prefs: readonly string[]): L;
};
export type { Store };
export type I18n<L extends string, K extends string> = {
    lang(): L;
    isLang(v: unknown): v is L;
    detectLang(prefs: readonly string[]): L;
    initialLang(prefs: readonly string[], store?: Store | null): L;
    setLang(l: L, store?: Store | null): void;
    onLang(f: () => void): () => void;
    t(key: K, params?: Params): string;
    tIn(l: L, key: K): string;
    lt(key: K, params?: Params): Text;
    lattr<E extends Element>(e: E, name: string, key: K, params?: Params): E;
};
export declare function createI18n<L extends string, K extends string>(o: I18nOptions<L, K>): I18n<L, K>;
