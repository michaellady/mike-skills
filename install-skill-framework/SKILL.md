---
name: install-skill-framework
description: Use when the user asks to install a skill or skill framework from a GitHub URL ("install https://github.com/...", "add the X framework", "pull in superpowers/gsd/bmad/speckit/openspec", "set up this framework").
---

# install-skill-framework

Install an external Claude Code skill framework from a GitHub URL. Handles the four shapes Mike has encountered: Claude Code plugin marketplace, `install.{sh,js}` scripts, flat `skills/` dirs, and one-off skills.

**PAIRED WITH:** `uninstall-skill` reads the install manifest this skill writes at `~/.claude/skill-installs/<framework>.json`.

## When to Use

Use when the user's message contains:
- `install https://github.com/...`
- "add/pull in the X framework/skills"
- "set up superpowers/gsd/bmad/speckit/openspec" (or any named framework repo)

Do NOT use when:
- Installing a single skill Mike is authoring — use `new-skill` for that
- The URL is a tool or library unrelated to Claude Code skills
- Mike wants to run the framework's install script manually (just run it directly)

## Workflow

1. **Resolve target directory.** Default is `~/dev/<repo-name>` (derived from the GitHub URL). If Mike specified "in this dir" and cwd is empty, use cwd.

2. **Clone:** `git clone <url> <target>`.

3. **Detect framework shape** — check these, in order:

   | Shape | Signal | Install via |
   |---|---|---|
   | Plugin marketplace | `.claude-plugin/` or `plugin.json` at root | Follow README — usually `claude plugin install` |
   | Install script | `install.sh`, `install.js`, `bin/install.*` | Dry-run the script, confirm, run |
   | Flat skills dir | `skills/<skill-name>/SKILL.md` layout | Symlink each skill into `~/.claude/skills/` |
   | Single skill | `SKILL.md` at root | Symlink repo root as `~/.claude/skills/<name>` |
   | Other | README has manual instructions | Follow README, ask Mike to confirm each step |

4. **For install-script shapes:** read the script first, print a one-sentence summary of what it will do (files touched, symlinks made, settings.json changes), and ask Mike via `AskUserQuestion` before running.

5. **For symlink shapes:** `ln -s <target>/skills/<name> ~/.claude/skills/<name>` for each skill. Skip any collisions and report them.

6. **Settings.json permissions.** If the framework's README or `plugin.json` declares hooks, commands, or tools that need permissions, update `~/.claude/settings.json` via the existing `update-config` skill.

7. **Write install manifest** at `~/.claude/skill-installs/<framework>.json`:
   ```json
   {
     "name": "<framework>",
     "source_url": "<github-url>",
     "target_dir": "<resolved path>",
     "installed_at": "<ISO timestamp>",
     "shape": "plugin|script|flat|single",
     "symlinks": ["~/.claude/skills/foo", "~/.claude/skills/bar"],
     "settings_changes": ["permissions.allow += ..."],
     "script_run": "install.sh"
   }
   ```
   This is what `uninstall-skill` reads to reverse the install cleanly.

8. **Verify.** Run `ls ~/.claude/skills/` and confirm the new symlinks resolve. If any are orphans, flag and stop.

9. **Report.** Summarize: framework name, shape detected, N skills installed, settings.json keys added (if any), and the install manifest path.

## Known Frameworks (Reference)

Mike has installed these five before. Use as priors for shape detection:

| Framework | URL | Shape | Notes |
|---|---|---|---|
| superpowers | github.com/obra/superpowers | plugin (has marketplace) OR flat skills | Also shippable as Claude plugin |
| gsd | github.com/gsd-build/get-shit-done | install script (`bin/install.js`) | Node-based installer |
| bmad | github.com/bmad-code-org/BMAD-METHOD | flat + AGENTS.md | Method-focused, not all skills |
| speckit | github.com/github/spec-kit | script + manual README steps | GitHub's spec-kit |
| openspec | github.com/Fission-AI/OpenSpec | flat | Smaller framework |

## Common Mistakes

- **Running an install script without dry-run first.** Unknown scripts may modify shell RC files or write into unexpected paths. Always summarize first.
- **Silent symlink collisions.** If `~/.claude/skills/foo` already exists, don't overwrite — report and ask.
- **Forgetting the install manifest.** Without it, `uninstall-skill` falls back to heuristics and may leave orphans.
- **Hardcoding `~/dev/` as the target.** If the user explicitly passed "in this dir" and cwd is a valid empty workspace, use cwd.
- **Installing before reading the README.** Every framework has its own quirks. The 30 seconds to scan the README prevents 10 minutes of debugging.

## Manifest Directory Setup

On first run, create `~/.claude/skill-installs/` if it doesn't exist. This directory is the source of truth for what's been installed via this skill.
