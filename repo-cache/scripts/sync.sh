#!/usr/bin/env bash
# Ensure a GitHub repo is cloned locally and reasonably fresh, then print its
# absolute local path on stdout.
#
# Usage:
#   sync.sh <owner>/<repo>            # default branch, shallow
#   sync.sh https://github.com/o/r    # URL form accepted
#   sync.sh <owner>/<repo>@<ref>      # specific branch/tag/sha
#
# Env:
#   REPO_CACHE_DIR    cache root (default: ~/.cache/claude-repo-cache)
#   REPO_CACHE_TTL_S  fetch only if last fetch older than this many seconds (default 86400)
#   REPO_CACHE_DEPTH  clone depth (default 1; set to 0 for full history)
#   REPO_CACHE_FORCE  if 1, always fetch regardless of TTL
#
# Stdout: one line — absolute local path of the working tree.
# Stderr: short status messages (cloning, fetching, up-to-date).
set -euo pipefail

input="${1:-}"
[ -z "$input" ] && { echo "usage: sync.sh <owner>/<repo>[@ref]" >&2; exit 2; }

cache_dir="${REPO_CACHE_DIR:-$HOME/.cache/claude-repo-cache}"
ttl="${REPO_CACHE_TTL_S:-86400}"
depth="${REPO_CACHE_DEPTH:-1}"
force="${REPO_CACHE_FORCE:-0}"

# Normalize input: strip protocol, trailing .git, fragment, extract optional @ref
spec="$input"
spec="${spec#https://github.com/}"
spec="${spec#http://github.com/}"
spec="${spec#git@github.com:}"
spec="${spec%.git}"
spec="${spec%/}"

ref=""
case "$spec" in
  *@*) ref="${spec##*@}"; spec="${spec%@*}" ;;
esac

owner="${spec%%/*}"
repo="${spec#*/}"
repo="${repo%%/*}"   # drop any trailing path components

if [ -z "$owner" ] || [ -z "$repo" ] || [ "$owner" = "$repo" ] && [ "${spec%/*}" = "$spec" ]; then
  echo "sync.sh: could not parse owner/repo from: $input" >&2; exit 2
fi

dest="$cache_dir/$owner/$repo"
mkdir -p "$cache_dir/$owner"

if [ ! -d "$dest/.git" ]; then
  echo "[repo-cache] cloning $owner/$repo → $dest" >&2
  if [ "$depth" = "0" ]; then
    git clone --quiet "https://github.com/$owner/$repo.git" "$dest"
  else
    git clone --quiet --depth "$depth" "https://github.com/$owner/$repo.git" "$dest"
  fi
else
  # Determine staleness from FETCH_HEAD mtime (fall back to HEAD).
  marker="$dest/.git/FETCH_HEAD"
  [ -f "$marker" ] || marker="$dest/.git/HEAD"
  now=$(date +%s)
  mtime=$(stat -f %m "$marker" 2>/dev/null || stat -c %Y "$marker" 2>/dev/null || echo 0)
  age=$((now - mtime))
  if [ "$force" = "1" ] || [ "$age" -ge "$ttl" ]; then
    echo "[repo-cache] fetching $owner/$repo (age=${age}s, ttl=${ttl}s)" >&2
    (cd "$dest" && git fetch --quiet --depth "${depth:-1}" origin 2>/dev/null \
      && git reset --quiet --hard "origin/$(git symbolic-ref --short HEAD 2>/dev/null || echo HEAD)" 2>/dev/null) || true
  else
    echo "[repo-cache] cache fresh ($owner/$repo, age=${age}s)" >&2
  fi
fi

if [ -n "$ref" ]; then
  echo "[repo-cache] checking out ref: $ref" >&2
  ( cd "$dest" \
    && git fetch --quiet origin "$ref" 2>/dev/null || true \
    && git checkout --quiet "$ref" 2>/dev/null \
       || git checkout --quiet "FETCH_HEAD" 2>/dev/null \
       || { echo "[repo-cache] WARN: could not check out $ref" >&2; } )
fi

printf '%s\n' "$dest"
