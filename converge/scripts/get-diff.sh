#!/usr/bin/env bash
# Get a base...HEAD diff, truncated to MAX_BYTES.
# Usage: get-diff.sh <base-branch> [<pr-number>]
# If <pr-number> is given, fetches the PR diff via gh instead.
# Truncates to ${CONVERGE_DIFF_MAX_BYTES:-51200} (50KB default) and appends a
# "[diff truncated at N bytes]" marker if it had to cut.
set -euo pipefail

base="${1:-}"
pr="${2:-}"
max="${CONVERGE_DIFF_MAX_BYTES:-51200}"

if [ -z "$base" ] && [ -z "$pr" ]; then
  echo "usage: $0 <base-branch> [<pr-number>]" >&2; exit 2
fi

if [ -n "$pr" ]; then
  diff=$(gh pr diff "$pr" 2>/dev/null) || { echo "get-diff: gh pr diff $pr failed" >&2; exit 1; }
else
  diff=$(git diff "${base}...HEAD" 2>/dev/null) || { echo "get-diff: git diff ${base}...HEAD failed" >&2; exit 1; }
fi

bytes=$(printf '%s' "$diff" | wc -c | tr -d ' ')
if [ "$bytes" -gt "$max" ]; then
  printf '%s' "$diff" | head -c "$max"
  printf '\n[diff truncated at %s bytes; full size %s bytes]\n' "$max" "$bytes"
else
  printf '%s\n' "$diff"
fi
