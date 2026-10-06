// Connection banner and fatal errors.
import type { Status } from "../net/socket.ts";
import { fill, h } from "./dom.ts";

/** The banner's texts, in the current language. */
export type BannerTexts = {
  updating(): string; // a new server version is taking over
  lost(): string;     // reconnecting
  conn(): string;     // a fatal error without a message
  home(): string;     // the way home
};

export class Banner {
  private readonly el: HTMLElement;
  private readonly texts: BannerTexts;
  private everOpen = false;
  private fatalShown = false;

  constructor(el: HTMLElement, texts: BannerTexts) {
    this.el = el;
    this.texts = texts;
  }

  /** Socket status: a reconnect notice once the game had been connected, or a server update. */
  status(s: Status): void {
    if (this.fatalShown) return;
    if (s === "open") this.everOpen = true;
    if (s === "updating") {
      this.show("info", h("span", { class: "spinner" }), this.texts.updating());
    } else if (s === "connecting" && this.everOpen) {
      this.show("info", h("span", { class: "spinner" }), this.texts.lost());
    } else {
      this.hide();
    }
  }

  /** A server error (already in the current language): the connection is over; offer the way home. */
  fatal(msg: string): void {
    this.fatalShown = true;
    this.show("error", h("strong", {}, msg || this.texts.conn()), h("a", { href: "/", class: "btn small" }, this.texts.home()));
  }

  hide(): void {
    this.el.hidden = true;
    this.el.className = "";
    fill(this.el);
  }

  private show(kind: "info" | "error", ...children: (Node | string)[]): void {
    this.el.className = kind;
    this.el.setAttribute("role", kind === "error" ? "alert" : "status");
    fill(this.el, ...children);
    this.el.hidden = false;
  }
}
