/** One fixed step of the game's simulation, ending at world tick `tick`. */
export type Model<S, I, E> = {
    step(s: S, inp: I, env: E, tick: number): S;
};
/** How a jump of the physics state is drawn. */
export interface Smoother<S> {
    rebase(drawnFrom: S, to: S): void;
    draw(s: S, dtS: number): S;
    reset(): void;
}
export declare class Reconciler<S, I, E> {
    private readonly model;
    private readonly smoother;
    private pending;
    private phys;
    private base;
    private recent;
    private shift;
    private snapTick;
    private ack;
    private lastAcked;
    private lastSeq;
    constructor(model: Model<S, I, E>, smoother: Smoother<S>, initial: S);
    /** World tick the step of input seq ends on, by the latest snapshot (the wind's clock). */
    tickFor(seq: number): number;
    /** Records a sent input and advances the predicted state by one tick (the step to world tick `tick`). */
    push(seq: number, inp: I, tick: number, env: E): void;
    /**
     * Rebases prediction on the server state of world tick snapTick, on which
     * the server applied seq ack: replays the inputs after ack, seq n as the
     * step to tickFor(n) (by seq, so inputs dropped past the cap do not shift
     * the wind ticks). Steps the server took beyond its acked inputs (shift)
     * are skipped from the replay, steps it dropped are replayed with the last
     * acked input, so queue timing noise does not move the drawn plane.
     */
    reconcile(server: S, ack: number, snapTick: number, env: E): void;
    /** World ticks the predicted state runs ahead of the latest snapshot's. */
    ahead(): number;
    /** Unacknowledged inputs kept for replay. */
    pendingCount(): number;
    /** Predicted physics state. */
    state(): S;
    /** Physics state plus the decaying visual correction; dtS = frame time. */
    render(dtS: number): S;
    /** Hard reset on spawn / death; snapTick and ack are the spawn snapshot's (baseline of the shift). */
    reset(s: S, snapTick?: number, ack?: number): void;
}
