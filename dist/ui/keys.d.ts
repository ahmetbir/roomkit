/** KeyboardEvent.code values; mouse buttons are "Mouse0" (left), "Mouse1" (middle), "Mouse2" (right). */
export type Codes = readonly string[];
export type KeyRow = [string, string];
type Mouse = "Mouse0" | "Mouse1" | "Mouse2";
/** "KeyW" → "W", "Mouse2" → mouse("Mouse2"), "Escape" → "Esc". */
export declare function keyName(code: string, mouse: (code: Mouse) => string): string;
/** The keys of several bindings, as a list label: "W / S" (duplicates once). */
export declare function keys(mouse: (code: Mouse) => string, ...codes: Codes[]): string;
export {};
