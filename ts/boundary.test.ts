import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const core = dirname(fileURLToPath(import.meta.url));

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? files(p) : p.endsWith(".ts") ? [p] : [];
  });
}

test("core imports only core (no game modules, no three)", () => {
  const bad: string[] = [];
  for (const f of files(core)) {
    const src = readFileSync(f, "utf8");
    for (const m of src.matchAll(/\b(?:from|import)\s*\(?\s*["']([^"']+)["']/g)) {
      const spec = m[1];
      if (spec.startsWith("node:") && f.endsWith(".test.ts")) continue; // tests only
      const target = resolve(dirname(f), spec);
      if (!spec.startsWith(".") || relative(core, target).startsWith("..")) bad.push(`${relative(core, f)} → ${spec}`);
    }
  }
  assert.deepEqual(bad, []);
});
