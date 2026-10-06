import { test } from "node:test";
import assert from "node:assert/strict";
import { keyName, keys } from "./keys.ts";
import { tabMove, trapIndex } from "./modal.ts";

test("tabMove: arrows wrap, Home and End jump, other keys are ignored", () => {
  assert.equal(tabMove("ArrowDown", 2, 3), 0);
  assert.equal(tabMove("ArrowRight", 0, 3), 1);
  assert.equal(tabMove("ArrowUp", 0, 3), 2);
  assert.equal(tabMove("ArrowLeft", 2, 3), 1);
  assert.equal(tabMove("Home", 2, 3), 0);
  assert.equal(tabMove("End", 0, 3), 2);
  assert.equal(tabMove("ArrowDown", 8, 9), 0);
  assert.equal(tabMove("ArrowUp", 0, 9), 8);
  assert.equal(tabMove("End", 2, 9), 8);
  assert.equal(tabMove("Enter", 2, 9), null);
  assert.equal(tabMove("x", 0, 3), null);
});

test("focus trap: Tab and Shift+Tab wrap inside the modal; outside focus enters at an end", () => {
  assert.equal(trapIndex(0, 3, false), 1);
  assert.equal(trapIndex(2, 3, false), 0);
  assert.equal(trapIndex(0, 3, true), 2);
  assert.equal(trapIndex(-1, 3, false), 0);
  assert.equal(trapIndex(-1, 3, true), 2);
});

test("key labels: letters, named keys, mouse buttons by the game's names, duplicates once", () => {
  const m = (c: string) => (c === "Mouse2" ? "R" : "M");
  assert.equal(keyName("KeyW", m), "W");
  assert.equal(keyName("Digit4", m), "4");
  assert.equal(keyName("Escape", m), "Esc");
  assert.equal(keyName("Mouse2", () => "R"), "R");
  assert.equal(keys(m, ["KeyW"], ["KeyS", "KeyW"]), "W / S");
  assert.equal(keys(m, ["Mouse2", "KeyE"]), "R / E");
});
