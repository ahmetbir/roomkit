import { fill, h } from "./dom.js";
export class Banner {
    el;
    texts;
    everOpen = false;
    fatalShown = false;
    constructor(el, texts) {
        this.el = el;
        this.texts = texts;
    }
    /** Socket status: a reconnect notice once the game had been connected, or a server update. */
    status(s) {
        if (this.fatalShown)
            return;
        if (s === "open")
            this.everOpen = true;
        if (s === "updating") {
            this.show("info", h("span", { class: "spinner" }), this.texts.updating());
        }
        else if (s === "connecting" && this.everOpen) {
            this.show("info", h("span", { class: "spinner" }), this.texts.lost());
        }
        else {
            this.hide();
        }
    }
    /** A server error (already in the current language): the connection is over; offer the way home. */
    fatal(msg) {
        this.fatalShown = true;
        this.show("error", h("strong", {}, msg || this.texts.conn()), h("a", { href: "/", class: "btn small" }, this.texts.home()));
    }
    hide() {
        this.el.hidden = true;
        this.el.className = "";
        fill(this.el);
    }
    show(kind, ...children) {
        this.el.className = kind;
        this.el.setAttribute("role", kind === "error" ? "alert" : "status");
        fill(this.el, ...children);
        this.el.hidden = false;
    }
}
