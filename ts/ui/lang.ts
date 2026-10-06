// The language switch (one button per language).
import type { I18n } from "../i18n/i18n.ts";
import { h } from "./dom.ts";

/** One toggle button per language; a click switches the language, then `switched` re-renders what is not live. */
export function langToggle<L extends string>(
  i: Pick<I18n<L, string>, "lang" | "setLang">,
  langs: readonly L[],
  label: () => string,
  title: (l: L) => string,
  switched: (l: L) => void = () => {},
): HTMLElement {
  const box = h("div", { class: "seg lang-toggle", role: "group", "aria-label": label() });
  const buttons = langs.map((l) => {
    const b = h("button", { type: "button", class: "seg-btn", lang: l, title: title(l), "aria-pressed": String(l === i.lang()) },
      l.toUpperCase());
    b.addEventListener("click", () => {
      if (l === i.lang()) return;
      for (const o of buttons) o.setAttribute("aria-pressed", String(o === b));
      i.setLang(l);
      box.setAttribute("aria-label", label());
      switched(l);
    });
    return b;
  });
  box.append(...buttons);
  return box;
}
