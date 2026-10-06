// Key labels for the key lists.
const NAMES = { ShiftLeft: "Shift", ShiftRight: "Shift", Escape: "Esc", ArrowLeft: "←", ArrowRight: "→" };
/** "KeyW" → "W", "Mouse2" → mouse("Mouse2"), "Escape" → "Esc". */
export function keyName(code, mouse) {
    if (code === "Mouse0" || code === "Mouse1" || code === "Mouse2")
        return mouse(code);
    return NAMES[code] ?? code.replace(/^(Key|Digit)/, "");
}
/** The keys of several bindings, as a list label: "W / S" (duplicates once). */
export function keys(mouse, ...codes) {
    return [...new Set(codes.flat().map((c) => keyName(c, mouse)))].join(" / ");
}
