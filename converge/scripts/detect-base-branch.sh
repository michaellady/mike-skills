#!/usr/bin/env bash
# Detect the base branch for `/converge review`.
# Order:
#   1) explicit PR number (arg 1) → gh pr view --json baseRefName
#   2) gh repo view default branch
#   3) origin/HEAD symbolic ref
#   4) origin/main, then origin/master
# Prints the base branch name on stdout (e.g. "main"). Exits 1 if none found.
set -euo pipefail

if [ "${1:-}" != "" ]; then
  if base=$(gh pr view "$1" --json baseRefName -q .baseRefName 2>/dev/null) && [ -n "$base" ]; then
    printf '%s\n' "$base"; exit 0
  fi
fi

if base=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null) && [ -n "$base" ]; then
  printf '%s\n' "$base"; exit 0
fi

if base=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null) && [ -n "$base" ]; then
  printf '%s\n' "${base#refs/remotes/origin/}"; exit 0
fi

if git rev-parse --verify origin/main >/dev/null 2>&1; then echo main; exit 0; fi
if git rev-parse --verify origin/master >/dev/null 2>&1; then echo master; exit 0; fi

echo "detect-base-branch: could not determine base branch" >&2
exit 1
