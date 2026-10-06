// UI language factory: a game gives its dictionaries (one complete source,
// the others may lack keys), its languages, its storage key and its detection
// rule. The returned functions are closures over the instance's state, so they
// are safe to destructure. Stored labels follow a switch via relabel; screens
// that build text on demand follow on their next render.
import { safeStore, type Store } from "../store.ts";
import { relabel, track } from "./live.ts";
import type { Msg, Params, Plural } from "./types.ts";

export type I18nOptions<L extends string, K extends string> = {
  dicts: Record<L, Readonly<Record<K, Msg>>>;
  source: L;                 // the complete dictionary; missing keys fall back to it
  langs: readonly L[];
  storeKey: string;          // e.g. "dogfight.lang"
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

export function createI18n<L extends string, K extends string>(o: I18nOptions<L, K>): I18n<L, K> {
  const fallback = o.dicts[o.source];
  /** The instance's language and its listeners (one page, one language). */
  const state: { lang: L; listeners: Set<() => void>; plural: Partial<Record<L, Intl.PluralRules>> } = {
    lang: o.source, listeners: new Set(), plural: {},
  };

  const lang = (): L => state.lang;
  const isLang = (v: unknown): v is L => o.langs.includes(v as L);
  const detectLang = (prefs: readonly string[]): L => o.detect(prefs);

  /** The stored choice, else the browser's preference. */
  function initialLang(prefs: readonly string[], store: Store | null = safeStore()): L {
    try {
      const v = store?.getItem(o.storeKey);
      if (isLang(v)) return v;
    } catch {
      // storage blocked: detect every visit
    }
    return detectLang(prefs);
  }

  /** Switches the language (stored, <html lang>, live labels, listeners); a no-op when unchanged. */
  function setLang(l: L, store: Store | null = safeStore()): void {
    try {
      store?.setItem(o.storeKey, l);
    } catch {
      // storage blocked: the choice lives for this page only
    }
    if (l === state.lang) return;
    state.lang = l;
    if (typeof document !== "undefined" && document.documentElement) document.documentElement.lang = l;
    relabel();
    for (const f of [...state.listeners]) f();
  }

  /** Calls f after every switch; returns the unsubscribe. */
  function onLang(f: () => void): () => void {
    state.listeners.add(f);
    return () => state.listeners.delete(f);
  }

  function plural(m: Plural, n: unknown): string {
    const rules = (state.plural[state.lang] ??= new Intl.PluralRules(state.lang));
    return rules.select(Number(n)) === "one" ? m.one : m.other;
  }

  /** The text of key in the current language; {name} placeholders take params, plural texts pick by params.n. */
  function t(key: K, params?: Params): string {
    const m = o.dicts[state.lang][key] ?? fallback[key];
    const s = typeof m === "string" ? m : plural(m, params?.n);
    if (!params) return s;
    return s.replace(/\{(\w+)\}/g, (all, k: string) => (k in params ? String(params[k]) : all));
  }

  /** The text of key in a given language (tests, the language toggle's own labels). */
  function tIn(l: L, key: K): string {
    const m = o.dicts[l][key];
    return typeof m === "string" ? m : m.other;
  }

  /** A text node that follows language switches (long-lived labels built once). */
  function lt(key: K, params?: Params): Text {
    return track(document.createTextNode(t(key, params)), (n) => { n.data = t(key, params); });
  }

  /** Sets attribute name of e to key's text, now and after every switch. */
  function lattr<E extends Element>(e: E, name: string, key: K, params?: Params): E {
    e.setAttribute(name, t(key, params));
    return track(e, (x) => x.setAttribute(name, t(key, params)));
  }

  return { lang, isLang, detectLang, initialLang, setLang, onLang, t, tIn, lt, lattr };
}
