import type { I18n } from "../i18n/i18n.js";
/** One toggle button per language; a click switches the language, then `switched` re-renders what is not live. */
export declare function langToggle<L extends string>(i: Pick<I18n<L, string>, "lang" | "setLang">, langs: readonly L[], label: () => string, title: (l: L) => string, switched?: (l: L) => void): HTMLElement;
