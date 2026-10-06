// Key labels for the key lists.

/** KeyboardEvent.code values; mouse buttons are "Mouse0" (left), "Mouse1" (middle), "Mouse2" (right). */
export type Codes = readonly string[];
export type KeyRow = [string, string];

type Mouse = "Mouse0" | "Mouse1" | "Mouse2";

const NAMES: Record<string, string> = { ShiftLeft: "Shift", ShiftRight: "Shift", Escape: "Esc", ArrowLeft: "←", ArrowRight: "→" };

/** "KeyW" → "W", "Mouse2" → mouse("Mouse2"), "Escape" → "Esc". */
export function keyName(code: string, mouse: (code: Mouse) => string): string {
  if (code === "Mouse0" || code === "Mouse1" || code === "Mouse2") return mouse(code);
  return NAMES[code] ?? code.replace(/^(Key|Digit)/, "");
}

/** The keys of several bindings, as a list label: "W / S" (duplicates once). */
export function keys(mouse: (code: Mouse) => string, ...codes: Codes[]): string {
  return [...new Set(codes.flat().map((c) => keyName(c, mouse)))].join(" / ");
}
