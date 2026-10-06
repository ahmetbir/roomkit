import type { Status } from "../net/socket.js";
/** The banner's texts, in the current language. */
export type BannerTexts = {
    updating(): string;
    lost(): string;
    conn(): string;
    home(): string;
};
export declare class Banner {
    private readonly el;
    private readonly texts;
    private everOpen;
    private fatalShown;
    constructor(el: HTMLElement, texts: BannerTexts);
    /** Socket status: a reconnect notice once the game had been connected, or a server update. */
    status(s: Status): void;
    /** A server error (already in the current language): the connection is over; offer the way home. */
    fatal(msg: string): void;
    hide(): void;
    private show;
}
