export declare class SessionModel<I> {
    private q;
    private last;
    private lastSeq;
    ack: number;
    push(seq: number, inp: I): void;
    /** The input for this tick and whether it is a fresh one (false: repeat or none yet). */
    next(): {
        inp: I | null;
        fresh: boolean;
    };
}
