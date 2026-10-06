// Compiles ts/ into the given directory (dist/ for the committed build, a
// temp dir for check.sh's freshness test) with the repo's tsconfig.
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";

const out = process.argv[2];
if (!out) throw new Error("usage: build-dist.mjs <outDir>");
rmSync(out, { recursive: true, force: true });
const tsc = createRequire(import.meta.url).resolve("typescript/bin/tsc");
execFileSync(process.execPath, [tsc, "-p", "tsconfig.json", "--outDir", out], { stdio: "inherit" });

// tsc rewrites relative ".ts" specifiers in the .js it emits but leaves them
// in .d.ts files, where a consumer's checker would look for a .ts beside
// them; point them at the emitted .js (whose .d.ts sits next to it).
function walk(dir) {
  for (const n of readdirSync(dir)) {
    const p = join(dir, n);
    if (statSync(p).isDirectory()) walk(p);
    else if (p.endsWith(".d.ts")) {
      const src = readFileSync(p, "utf8");
      const fixed = src.replace(/(from\s+"\.{1,2}\/[^"]*?)\.ts"/g, '$1.js"');
      if (fixed !== src) writeFileSync(p, fixed);
    }
  }
}
walk(out);
