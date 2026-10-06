// The centered error card shown when a session cannot start.
import { fill, h } from "./dom.js";
/** A centered error card (before the game started) with the way home. */
export function errorCard(ui, brand, home, title, msg, ...actions) {
    fill(ui, h("div", { class: "screen" }, h("header", { class: "brand" }, h("h1", {}, brand)), h("div", { class: "panel narrow error-card", role: "alert" }, h("h2", {}, title), h("p", {}, msg), ...actions, h("a", { href: "/", class: actions.length ? "btn" : "btn primary" }, home))));
}
