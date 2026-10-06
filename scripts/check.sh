#!/usr/bin/env bash
# Everything a change to roomkit must pass: Go vet + tests (race), the TS
# type check and tests, and a committed dist/ that matches a fresh build.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "check: go"
go vet ./...
go test -race -count=1 ./...

echo "check: ts"
npm run --silent check
npm test --silent

echo "check: dist is current"
tmp="$(mktemp -d)"
trap 'rm -r "$tmp"' EXIT
node scripts/build-dist.mjs "$tmp/dist" >/dev/null
if ! diff -r dist "$tmp/dist" >/dev/null; then
  echo "check: dist/ differs from a fresh build; run npm run build and commit it" >&2
  diff -r dist "$tmp/dist" | head -20 >&2
  exit 1
fi
echo "check: ok"
