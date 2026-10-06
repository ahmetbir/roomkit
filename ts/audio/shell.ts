// The audio shell of a game: the AudioContext is created on the first user
// gesture (autoplay policy) and suspended while the tab is hidden; every call
// before that is a no-op. The game builds its continuous voices once, on the
// created context; one-shot noise bursts and tones are played here.

/** The game's continuous voices, built once when the context exists. */
export type Voices = { start(ctx: AudioContext, master: GainNode, noise: AudioBuffer): void };
export type Filter = { type: BiquadFilterType; f0: number; f1?: number; q: number };

// The compressor adds automatic makeup gain: +4.33 dB below threshold for the
// limiter settings below (measured in Chromium with an OfflineAudioContext).
// Undo it so the mix keeps its pre-limiter loudness.
const LIMITER_TRIM = 10 ** (-4.33 / 20);
const SMOOTH = 0.08; // s, setTargetAtTime time constant

/** Gain for a sound d meters away. */
export function falloff(d: number): number {
  return 1 / (1 + Math.max(0, d) / 500);
}

type Win = Pick<Window, "addEventListener" | "removeEventListener">;
type Doc = Pick<Document, "addEventListener" | "removeEventListener" | "hidden">;

export class AudioShell {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private noise: AudioBuffer | null = null;
  private vol = 0.8;
  private readonly voices: Voices;
  private readonly doc: Doc;
  private readonly off: (() => void)[] = [];

  constructor(voices: Voices, win: Win = window, doc: Doc = document) {
    this.voices = voices;
    this.doc = doc;
    const unlock = () => this.start();
    const vis = () => {
      if (!this.ctx) return;
      void (this.doc.hidden ? this.ctx.suspend() : this.ctx.resume()).catch(() => {});
    };
    win.addEventListener("pointerdown", unlock);
    win.addEventListener("keydown", unlock);
    doc.addEventListener("visibilitychange", vis);
    this.off.push(
      () => win.removeEventListener("pointerdown", unlock),
      () => win.removeEventListener("keydown", unlock),
      () => doc.removeEventListener("visibilitychange", vis),
    );
  }

  setVolume(v: number): void {
    this.vol = Math.max(0, Math.min(1, v));
    if (this.ctx && this.master) this.master.gain.setTargetAtTime(this.vol, this.ctx.currentTime, SMOOTH);
  }

  /** The context once a gesture created it (running or suspended), else null. */
  context(): AudioContext | null {
    return this.ctx;
  }

  /** A running context. A suspended one (hidden tab) has a frozen currentTime:
   *  every one-shot scheduled then would pile up and sound at once on return. */
  live(): boolean {
    return this.ctx?.state === "running";
  }

  /** One-shot filtered noise: attack, then exponential decay over seconds. */
  burst(fl: Filter, seconds: number, gain: number, attack = 0.002): void {
    const c = this.ctx;
    if (!c || !this.live() || !this.master || !this.noise || gain < 0.003) return;
    const t = c.currentTime;
    const src = c.createBufferSource();
    src.buffer = this.noise;
    const rate = 0.8 + Math.random() * 0.4;
    src.playbackRate.value = rate;
    const filt = c.createBiquadFilter();
    filt.type = fl.type;
    filt.Q.value = fl.q;
    filt.frequency.setValueAtTime(fl.f0, t);
    if (fl.f1) filt.frequency.exponentialRampToValueAtTime(fl.f1, t + seconds);
    const g = c.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(gain, t + attack);
    g.gain.exponentialRampToValueAtTime(0.0001, t + seconds);
    src.connect(filt).connect(g).connect(this.master);
    // Random offset, but leave enough buffer for the whole sound at this rate.
    const room = Math.max(0, this.noise.duration - (seconds + 0.05) * rate);
    src.start(t, Math.random() * room);
    src.stop(t + seconds + 0.05);
  }

  /** One-shot oscillator sweep f0 → f1, starting delay seconds from now. */
  tone(type: OscillatorType, f0: number, f1: number, seconds: number, gain: number, delay = 0): void {
    const c = this.ctx;
    if (!c || !this.live() || !this.master) return;
    const t = c.currentTime + delay;
    const o = c.createOscillator();
    o.type = type;
    o.frequency.setValueAtTime(f0, t);
    if (f1 !== f0) o.frequency.exponentialRampToValueAtTime(f1, t + seconds);
    const g = c.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(gain, t + 0.005);
    g.gain.exponentialRampToValueAtTime(0.0001, t + seconds);
    o.connect(g).connect(this.master);
    o.start(t);
    o.stop(t + seconds + 0.05);
  }

  dispose(): void {
    for (const f of this.off) f();
    this.off.length = 0;
    void this.ctx?.close().catch(() => {});
    this.ctx = null;
  }

  private start(): void {
    if (this.ctx) {
      if (this.ctx.state === "suspended" && !this.doc.hidden) void this.ctx.resume().catch(() => {});
      return;
    }
    let c: AudioContext;
    try {
      c = new AudioContext();
    } catch {
      return; // no Web Audio: stay silent
    }
    this.ctx = c;
    const master = c.createGain();
    master.gain.value = this.vol;
    this.master = master;
    const trim = c.createGain();
    trim.gain.value = LIMITER_TRIM;
    master.connect(this.limiter(c)).connect(trim).connect(c.destination);
    const n = c.createBuffer(1, c.sampleRate * 2, c.sampleRate);
    const d = n.getChannelData(0);
    for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
    this.noise = n;
    this.voices.start(c, master, n);
    if (this.doc.hidden) void c.suspend().catch(() => {});
  }

  /** Soft limiter (its automatic makeup gain is undone by LIMITER_TRIM): unity below ≈ −10 dBFS, so stacked flybys + AB + an explosion do not clip. */
  private limiter(c: AudioContext): DynamicsCompressorNode {
    const k = c.createDynamicsCompressor();
    k.threshold.value = -10;
    k.knee.value = 6;
    k.ratio.value = 12;
    k.attack.value = 0.003;
    k.release.value = 0.25;
    return k;
  }
}
