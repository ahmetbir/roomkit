/** The game's continuous voices, built once when the context exists. */
export type Voices = {
    start(ctx: AudioContext, master: GainNode, noise: AudioBuffer): void;
};
export type Filter = {
    type: BiquadFilterType;
    f0: number;
    f1?: number;
    q: number;
};
/** Gain for a sound d meters away. */
export declare function falloff(d: number): number;
type Win = Pick<Window, "addEventListener" | "removeEventListener">;
type Doc = Pick<Document, "addEventListener" | "removeEventListener" | "hidden">;
export declare class AudioShell {
    private ctx;
    private master;
    private noise;
    private vol;
    private readonly voices;
    private readonly doc;
    private readonly off;
    constructor(voices: Voices, win?: Win, doc?: Doc);
    setVolume(v: number): void;
    /** The context once a gesture created it (running or suspended), else null. */
    context(): AudioContext | null;
    /** A running context. A suspended one (hidden tab) has a frozen currentTime:
     *  every one-shot scheduled then would pile up and sound at once on return. */
    live(): boolean;
    /** One-shot filtered noise: attack, then exponential decay over seconds. */
    burst(fl: Filter, seconds: number, gain: number, attack?: number): void;
    /** One-shot oscillator sweep f0 → f1, starting delay seconds from now. */
    tone(type: OscillatorType, f0: number, f1: number, seconds: number, gain: number, delay?: number): void;
    dispose(): void;
    private start;
    /** Soft limiter (its automatic makeup gain is undone by LIMITER_TRIM): unity below ≈ −10 dBFS, so stacked flybys + AB + an explosion do not clip. */
    private limiter;
}
export {};
