/** Blends two states; u in [0, 1]. */
export type Mix<S> = (a: S, b: S, u: number) => S;
/** Carries a state along its own motion for the given seconds. */
export type Ahead<S> = (s: S, seconds: number) => S;
type Entry<S> = {
    t: number;
    fs: S;
};
export declare class InterpBuffer<S> {
    private entries;
    private readonly mix;
    constructor(mix: Mix<S>);
    /** Adds a snapshot state; out-of-order or duplicate times are ignored. */
    push(serverTimeMs: number, fs: S): void;
    /** The latest entry (server time ms and state). */
    newest(): Entry<S> | undefined;
    size(): number;
    /** The mix of the neighbours at renderTimeMs; clamps at the ends. */
    sample(renderTimeMs: number): S | null;
}
/**
 * The state at serverTimeMs: interpolated inside the buffer, carried along
 * the newest state's own motion past its end (at most MAX_AHEAD_MS), e.g. a
 * target's present position for a gunsight while the drawn one lags behind.
 */
export declare function extrapolate<S>(buf: InterpBuffer<S>, serverTimeMs: number, ahead: Ahead<S>): S | null;
/**
 * ServerClock maps local time to server time. The offset tracks the lowest
 * observed delay (now − serverMs) and relaxes by 5 ms every 5 s so it can
 * follow a slowly rising delay. tickMs is the game's tick length, delayMs
 * how far behind the server remote entities are drawn.
 */
export declare class ServerClock {
    private offset;
    private relaxedAt;
    private readonly tickMs;
    private readonly delayMs;
    constructor(tickMs?: number, delayMs?: number);
    /** Records a snapshot of tick received at local time nowMs. */
    observe(tick: number, nowMs: number): void;
    /** Server time (ms) for a tick. */
    serverMs(tick: number): number;
    /** Server time to render remote entities at. */
    renderTime(nowMs: number): number;
}
export {};
