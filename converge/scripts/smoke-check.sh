#!/usr/bin/env bash
# Run a project-appropriate smoke check.
# Usage: smoke-check.sh [build|test]   (default: build)
# Detects project type by marker files (in priority order) and runs the matching
# command. Prints "PASS" or "FAIL" on stdout, with the failing tail on stderr.
# Exits 0 on PASS, 1 on FAIL, 2 if no project type detected.
set -uo pipefail

mode="${1:-build}"
case "$mode" in build|test) ;; *) echo "usage: $0 [build|test]" >&2; exit 2 ;; esac

# Allow override via env: CONVERGE_SMOKE_BUILD / CONVERGE_SMOKE_TEST
if [ "$mode" = "build" ] && [ -n "${CONVERGE_SMOKE_BUILD:-}" ]; then cmd="$CONVERGE_SMOKE_BUILD"
elif [ "$mode" = "test" ] && [ -n "${CONVERGE_SMOKE_TEST:-}" ]; then cmd="$CONVERGE_SMOKE_TEST"
elif [ -f go.mod ]; then
  cmd=$([ "$mode" = build ] && echo "go build ./..." || echo "go test ./...")
elif [ -f Cargo.toml ]; then
  cmd=$([ "$mode" = build ] && echo "cargo check" || echo "cargo test")
elif [ -f package.json ]; then
  if jq -e '.scripts.build' package.json >/dev/null 2>&1 && [ "$mode" = build ]; then
    cmd="npm run build"
  elif jq -e '.scripts.test' package.json >/dev/null 2>&1 && [ "$mode" = test ]; then
    cmd="npm test"
  elif [ -f tsconfig.json ] && [ "$mode" = build ]; then
    cmd="npx tsc --noEmit"
  else
    echo "smoke-check: package.json present but no \`$mode\` script" >&2; exit 2
  fi
elif [ -f pyproject.toml ] || [ -f setup.py ]; then
  cmd=$([ "$mode" = build ] && echo "python -m compileall -q ." || echo "pytest -q")
else
  echo "smoke-check: no recognized project type (go.mod, Cargo.toml, package.json, pyproject.toml)" >&2
  exit 2
fi

log=$(mktemp -t converge-smoke.XXXXXX)
trap 'rm -f "$log"' EXIT

if bash -c "$cmd" >"$log" 2>&1; then
  echo "PASS (cmd: $cmd)"
  exit 0
else
  rc=$?
  echo "FAIL (cmd: $cmd, exit: $rc)"
  echo "--- last 40 lines ---" >&2
  tail -40 "$log" >&2
  exit 1
fi
