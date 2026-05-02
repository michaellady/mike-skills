#!/usr/bin/env bash
# Resolve the plan file for `/converge plan`.
# Precedence:
#   1) explicit path argument (must exist)
#   2) $CONVERGE_ACTIVE_PLAN env var (the caller can set this from conversation context)
#   3) most-recently-modified *.md in ~/.claude/plans/ matching the current repo slug
#   4) most-recently-modified *.md in ~/.claude/plans/
# Prints the absolute path on stdout. Exits 1 with a message on stderr if nothing found.
set -euo pipefail

PLANS_DIR="${CLAUDE_PLANS_DIR:-$HOME/.claude/plans}"

if [ "${1:-}" != "" ]; then
  if [ -f "$1" ]; then
    cd "$(dirname "$1")" && printf '%s\n' "$(pwd)/$(basename "$1")"
    exit 0
  fi
  echo "resolve-plan: explicit path not found: $1" >&2
  exit 1
fi

if [ -n "${CONVERGE_ACTIVE_PLAN:-}" ] && [ -f "$CONVERGE_ACTIVE_PLAN" ]; then
  printf '%s\n' "$CONVERGE_ACTIVE_PLAN"
  exit 0
fi

if [ ! -d "$PLANS_DIR" ]; then
  echo "resolve-plan: no plans dir at $PLANS_DIR" >&2
  exit 1
fi

REPO_SLUG=""
if git rev-parse --show-toplevel >/dev/null 2>&1; then
  REPO_SLUG=$(basename "$(git rev-parse --show-toplevel)")
fi

if [ -n "$REPO_SLUG" ]; then
  match=$(find "$PLANS_DIR" -maxdepth 3 -type f -name "*.md" 2>/dev/null \
    | grep -i -- "$REPO_SLUG" \
    | xargs -I{} stat -f "%m %N" {} 2>/dev/null \
    | sort -rn | head -1 | cut -d' ' -f2-)
  if [ -n "$match" ]; then printf '%s\n' "$match"; exit 0; fi
fi

match=$(find "$PLANS_DIR" -maxdepth 3 -type f -name "*.md" 2>/dev/null \
  | xargs -I{} stat -f "%m %N" {} 2>/dev/null \
  | sort -rn | head -1 | cut -d' ' -f2-)

if [ -z "$match" ]; then
  echo "resolve-plan: no .md files in $PLANS_DIR" >&2
  exit 1
fi

printf '%s\n' "$match"
