#!/usr/bin/env bash
# Print the local cache path for a GitHub repo without fetching/cloning.
# Use this to ask "is it cached, and where?" before deciding to sync.
# Exits 0 with path on stdout if present (regardless of staleness), 1 if not cloned yet.
set -euo pipefail

input="${1:-}"
[ -z "$input" ] && { echo "usage: path.sh <owner>/<repo>" >&2; exit 2; }

cache_dir="${REPO_CACHE_DIR:-$HOME/.cache/claude-repo-cache}"
spec="$input"
spec="${spec#https://github.com/}"; spec="${spec#http://github.com/}"; spec="${spec#git@github.com:}"
spec="${spec%.git}"; spec="${spec%/}"; spec="${spec%@*}"

owner="${spec%%/*}"; repo="${spec#*/}"; repo="${repo%%/*}"
dest="$cache_dir/$owner/$repo"

if [ -d "$dest/.git" ]; then
  printf '%s\n' "$dest"
  exit 0
fi
exit 1
