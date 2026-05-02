---
name: repo-cache
description: Use whenever the user references a GitHub repo by URL or owner/repo and you'd otherwise make repeated `gh api repos/.../contents` calls. Clones the repo into `~/.cache/claude-repo-cache/<owner>/<repo>` (shallow) and returns a local path so subsequent exploration uses Read/Grep/find on local files. Triggers — any github.com URL the user wants explored, "look at <owner>/<repo>", "what's in the X repo", "compare to <gh-url>", "find Y in <repo>".
user_invocable: false
---

# repo-cache

Replace repeated `gh api repos/.../contents` calls with a one-time shallow clone, then use normal local tools (Read, Grep, find, ripgrep) on the cached path.

## When to use

Whenever any of these hold:

- The user gives a `github.com/<owner>/<repo>` URL or says "look at `<owner>/<repo>`".
- You're about to make a 2nd `gh api repos/.../contents/...` call against the same repo.
- The user asks to compare/diff/study/inspect a public GitHub repo's structure.

If you only need a single specific file and won't touch the repo again, `gh api` once is fine — don't bother caching.

## What this skill is NOT

- Not for the user's own work-in-progress repos (those are already on disk via their normal checkouts).
- Not for private repos that need authenticated `git clone` setup the user hasn't done — fall back to `gh api`.
- Not a substitute for `git worktree` when the user wants to actually edit the code.

## How to use

### 1. Sync (clone or refresh)

```bash
~/.claude/skills/repo-cache/scripts/sync.sh <owner>/<repo>
# prints absolute local path on stdout
# stderr: "[repo-cache] cloning ..." / "fetching ..." / "cache fresh ..."
```

Accepted input forms (all normalized):

- `openai/codex-plugin-cc`
- `https://github.com/openai/codex-plugin-cc`
- `https://github.com/openai/codex-plugin-cc.git`
- `openai/codex-plugin-cc@<branch|tag|sha>`

Capture the printed path:

```bash
REPO=$(~/.claude/skills/repo-cache/scripts/sync.sh openai/codex-plugin-cc)
```

### 2. Search locally

Once `$REPO` is set, use the regular tools:

- **Tree / find:** `find "$REPO" -maxdepth 3 -type f` (faster than recursive `gh api`)
- **Grep:** `rg -n "<pattern>" "$REPO"` (or `grep -rn` if `rg` isn't installed)
- **Read a file:** Read tool with absolute path `$REPO/<path>`
- **List a dir:** `ls "$REPO/<subdir>"` — beats paginated `gh api .../contents` calls

### 3. Idempotent re-use

`sync.sh` is safe to call repeatedly:

- If the repo isn't cloned, it clones (shallow, depth 1).
- If it's been fetched within `REPO_CACHE_TTL_S` (default 24h), it's a no-op fast path.
- If older than the TTL, it does `git fetch && git reset --hard origin/<default>`.

So you can call it at the top of every step that needs the repo without worrying about overhead.

## Configuration (env vars)

| Var | Default | Purpose |
|---|---|---|
| `REPO_CACHE_DIR` | `~/.cache/claude-repo-cache` | Cache root |
| `REPO_CACHE_TTL_S` | `86400` (24h) | Fetch only if older than this |
| `REPO_CACHE_DEPTH` | `1` | Clone depth (set `0` for full history if you need git log/blame) |
| `REPO_CACHE_FORCE` | `0` | If `1`, always fetch regardless of TTL |

If the user wants commit history, blame, or to look across many tags, re-sync once with `REPO_CACHE_DEPTH=0`:

```bash
REPO_CACHE_DEPTH=0 REPO_CACHE_FORCE=1 ~/.claude/skills/repo-cache/scripts/sync.sh <owner>/<repo>
```

## Helper scripts

| Script | Purpose |
|---|---|
| `scripts/sync.sh <spec>` | Clone or refresh; prints local path on stdout, status on stderr. |
| `scripts/path.sh <spec>` | Print local path if already cloned (no network), else exit 1. |

## Failure modes

- **Private repo without auth:** `git clone` fails. Fall back to `gh api` (which uses the user's token) and tell the user that authenticated cloning isn't set up.
- **Network down:** if the repo is already cached, `sync.sh` will fail to fetch but still print the cached path — and you can keep working on possibly-stale content.
- **Disk pressure:** the cache lives under `~/.cache/`. If it grows, it's safe to `rm -rf` any subtree — `sync.sh` will re-clone on next call.
- **Spec parse fails:** prints `sync.sh: could not parse owner/repo` and exits 2. Re-prompt the user for an unambiguous form (`<owner>/<repo>`).

## Example: replacing a chain of `gh api` calls

Before (one `gh api` per directory level — slow, paginated, hits rate limits):

```bash
gh api repos/openai/codex-plugin-cc/contents | ...
gh api repos/openai/codex-plugin-cc/contents/plugins | ...
gh api repos/openai/codex-plugin-cc/contents/plugins/codex | ...
gh api repos/openai/codex-plugin-cc/contents/plugins/codex/scripts | ...
```

After (one clone, then local FS):

```bash
REPO=$(~/.claude/skills/repo-cache/scripts/sync.sh openai/codex-plugin-cc)
find "$REPO" -maxdepth 4 -type f | head -40
rg -n "codex exec" "$REPO"
```
