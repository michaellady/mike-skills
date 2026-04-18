---
name: uninstall-skill
description: Use when the user wants to remove a Claude Code skill or skill framework ("uninstall X", "remove the X skill", "get rid of superpowers/gsd/bmad", "clean up broken skill", "undo last install").
---

# uninstall-skill

Cleanly remove a Claude Code skill or framework. Destructive by nature — always dry-runs first, always requires explicit confirmation, and leaves an undo log.

**PAIRED WITH:** Reads manifests written by `install-skill-framework` at `~/.claude/skill-installs/<name>.json`. Falls back to heuristic detection when no manifest exists.

**SAFETY:** This skill performs filesystem deletions. You MUST follow the dry-run → confirm → execute workflow. Do NOT skip the confirmation step even if Mike's message sounds emphatic.

## When to Use

Use when the user asks:
- "uninstall X" / "remove the X skill"
- "get rid of the superpowers/gsd/bmad framework"
- "clean up orphan skills" (follow up from `skill-audit`)
- "undo last install"

Do NOT use when:
- The skill is still being developed — just delete the workspace dir
- The user wants to RENAME a skill — use `mv` or edit frontmatter directly
- The user wants to DISABLE a skill temporarily — move to a quarantine dir instead (non-destructive)

## Workflow

1. **Locate the target.** Check in this order:
   - `~/.claude/skill-installs/<name>.json` — authoritative manifest
   - `~/.claude/skills/<name>` — global skill (symlink or dir)
   - `~/dev/<name>/` — source dir (framework-level)
   - `$CWD/.claude/skills/<name>` — project-local

   If not found, stop and ask Mike to clarify.

2. **Enumerate removal candidates:**

   **With manifest:** read the JSON and list every `symlinks[]`, `settings_changes[]`, and `target_dir` the install recorded.

   **Without manifest (heuristic mode):**
   - For a single skill: the dir/symlink at `~/.claude/skills/<name>` plus any matching `~/dev/<name>/` source.
   - For a framework: scan `~/.claude/skills/` for symlinks whose targets are under `~/dev/<name>/`. Add all of them.
   - Check `~/.claude/settings.json` for hooks/commands/permissions referencing `<name>`.

3. **Build a dry-run report.** Show Mike exactly what will change:

   ```
   Removing framework: gsd
   - Symlinks to remove (4):
       ~/.claude/skills/gsd:brainstorm
       ~/.claude/skills/gsd:plan
       ~/.claude/skills/gsd:ship
       ~/.claude/skills/gsd:review
   - Source dir to remove: ~/dev/gsd/
   - settings.json changes:
       permissions.allow -= "Bash(gsd:*)"
       hooks.SessionStart -= "gsd/hooks/startup.sh"
   - Manifest to remove: ~/.claude/skill-installs/gsd.json
   Total disk space reclaimed: ~12MB
   ```

4. **Require confirmation via `AskUserQuestion`.** Options:
   - "Yes, remove everything listed"
   - "Remove symlinks and settings only (keep source dir)"
   - "Cancel"

   Do NOT proceed on a text "yes" — use the structured question.

5. **Write undo log** BEFORE making changes at `/Users/mikelady/dev/mike-skills/uninstall-logs/<name>-<YYYYMMDD-HHMMSS>.md`:
   - Exact list of what was removed
   - Original symlink targets (so Mike can recreate them if needed)
   - settings.json diff (before/after relevant keys)

6. **Execute removal** in this order (least-destructive first):
   1. Remove symlinks (`rm` — symlinks only, never -rf)
   2. Update settings.json via `update-config` skill
   3. Remove install manifest
   4. Remove source dir LAST (and only if Mike chose "remove everything"). Use `rm -rf <dir>` only after confirming the path is under `~/dev/` or `~/.claude/skills/`.

7. **Verify.** Run `ls ~/.claude/skills/` and confirm none of the removed names appear. If any remain, report and stop.

8. **Report** what was removed and where the undo log lives.

## Safety Rails

- **Never use `rm -rf` without confirming the path.** Must match `^(/Users/mikelady/dev/|/Users/mikelady/\.claude/)` — refuse otherwise.
- **Never touch `~/.claude/plans/` or `~/.claude/projects/`.** Those are user data.
- **Never delete `~/.claude/settings.json` entirely.** Only remove specific keys via `update-config`.
- **Never skip the dry-run.** Even if Mike says "just do it" — show the plan, require structured confirmation.
- **Never remove a skill Mike is actively editing** (check if there's an open session with cwd under the skill path — rare, but worth a heads-up).

## Heuristic Framework Detection

When no manifest exists, detect framework membership via:

| Signal | How |
|---|---|
| Symlink target | Resolve `~/.claude/skills/*` targets; group by source dir |
| Name prefix | `dialed:*`, `peon-ping-*`, `superpowers:*` suggest framework |
| Frontmatter source | Some skills declare `source: <url>` in frontmatter |
| README in source dir | `~/dev/<framework>/README.md` confirms it's a known framework |

If heuristics disagree, show all candidates and let Mike choose via `AskUserQuestion`.

## Common Mistakes

- **Skipping the undo log.** Without it, recovery after a mistaken uninstall is painful.
- **Removing source dir before symlinks.** Leaves orphan symlinks. Remove symlinks first.
- **Using `rm -rf` on a symlink.** It resolves the link and deletes the target dir. Use `rm` (no -r, no -f) on symlinks.
- **Leaving settings.json stale.** Hooks/permissions referencing removed skills cause errors on next session start.
- **Proceeding without structured confirmation.** Even an emphatic "yes" in chat can be misread — use `AskUserQuestion`.
