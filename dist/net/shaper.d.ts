/** Every message has a type tag. */
export type Envelope = {
    t: string;
};
/** The game's outbound shaping rules. */
export type ShaperPolicy<M extends Envelope> = {
    isInput(m: M): boolean;
    latch(held: M, next: M): M;
    gapped(m: M): boolean;
    gapMs: number;
};
/** Bytes queued in the browser above which the connection counts as backed up. */
export declare const MAX_BUFFERED: number;
/** Server silence (snaps come at 30 Hz) after which the network counts as stalled. */
export declare const STALL_MS = 1000;
export type ShaperEnv<M extends Envelope> = {
    now: () => number;
    setTimeout: (f: () => void, ms: number) => unknown;
    clearTimeout: (h: unknown) => void;
    buffered: () => number;
    write: (m: M) => boolean;
};
/**
 * Shaper sits between the game and one open connection. While the connection
 * is backed up it holds only the newest input (one-shot presses of the ones
 * it replaces carry over) and sends it once traffic flows again; the server
 * keeps only the newest inputs anyway. Gapped messages go out at most once
 * per policy.gapMs. Every message it accepts reports true.
 */
export declare class Shaper<M extends Envelope> {
    private readonly env;
    private readonly p;
    private lastRecv;
    private heldIn;
    private lastGap;
    private heldGap;
    private gapTimer;
    constructor(env: ShaperEnv<M>, policy: ShaperPolicy<M>);
    /** Any server message: the network is flowing; a held input goes out. */
    received(): void;
    send(m: M): boolean;
    /** The connection is gone: nothing held is sent. */
    dispose(): void;
    private backedUp;
    private input;
    private gap;
}
