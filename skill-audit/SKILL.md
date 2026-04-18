---
name: skill-audit
description: Use when the user wants an inventory of installed Claude Code skills, to find duplicates, orphan symlinks, trigger overlaps, or unknown-origin skills ("audit my skills", "what skills do I have", "skill inventory", "find orphan/duplicate skills", "what framework does X come from").
---

# skill-audit

Inventory every Claude Code skill visible to the current environment. Identifies origin, groups by framework, and flags problems (broken symlinks, duplicate names, overlapping triggers) that Mike should resolve.

## When to Use

Use when the user asks:
- "audit my skills" / "what skills do I have"
- "is there a duplicate/conflict between X and Y?"
- "find orphan skills" / "broken symlinks in ~/.claude/skills"
- "what did framework X install?"

Do NOT use when:
- The user wants to remove a specific skill — use `uninstall-skill`
- The user wants to install a framework — use `install-skill-framework`
- The user wants a list of ALL available skills including built-ins (this skill only covers filesystem skills, not runtime-loaded skills)

## Inventory Sources (in order)

1. `~/.claude/skills/` — global user skills
2. `~/.claude/skill-installs/*.json` — install manifests (written by `install-skill-framework`)
3. `$CWD/.claude/skills/` — project-local skills, if present
4. `/Users/mikelady/dev/mike-skills/` — workspace (authored, not yet installed)

## Workflow

1. **Enumerate skills** across all four sources. For each, record: name, path, is_symlink, resolved target.

2. **Resolve origin** for each skill:
   - If symlink → resolve target, check if it's under a known framework dir (`~/dev/superpowers`, `~/dev/gsd`, `~/dev/bmad`, `~/dev/speckit`, `~/dev/openspec`, `~/dev/mike-skills`).
   - Else if regular dir → check frontmatter for explicit source, else tag "locally authored".
   - Cross-reference with `~/.claude/skill-installs/*.json` manifests for extra context.

3. **Detect issues:**
   - **Orphans:** symlinks whose target no longer exists.
   - **Duplicates:** two skills with the same `name:` in frontmatter (even if in different dirs).
   - **Trigger overlap:** skills whose `description:` trigger phrases share substantial overlap (e.g., both mention "deploy").
   - **Name vs. dir mismatch:** the directory name doesn't match the frontmatter `name:` (will cause invocation confusion).

4. **Group by origin** (framework vs. locally authored vs. unknown).

5. **Report as a table**, then a list of issues. Example output:

   ```
   Source                      Count
   superpowers                    9
   dialed:* framework             4
   peon-ping-* family             4
   locally authored (global)     23
   mike-skills workspace          5
   project-local                  0
   ------------------------------
   Total visible                 45

   Issues:
   ⚠️  Orphan: ~/.claude/skills/old-skill → target missing
   ⚠️  Duplicate name 'review': in superpowers/ AND in ~/.claude/skills/review/
   ⚠️  Trigger overlap: `ship` and `land-and-deploy` both trigger on "deploy"
   ⚠️  Name mismatch: dir 'pr-review' has frontmatter name 'review'
   ```

6. **Offer next steps:** for each issue, suggest the command to fix it (usually `uninstall-skill` or a manual rename).

## Frontmatter Parsing

Parse the YAML frontmatter with a simple regex or a Python one-liner — don't pull in a YAML library. Only two fields matter for audit: `name` and `description`.

```python
import re, sys
for path in paths:
    text = open(path).read()
    m = re.match(r'^---\n(.*?)\n---', text, re.DOTALL)
    if not m: continue
    fm = m.group(1)
    name = re.search(r'^name:\s*(.+)$', fm, re.M)
    desc = re.search(r'^description:\s*(.+)$', fm, re.M)
```

## Common Mistakes

- **Treating directory name as skill name.** The canonical name is in frontmatter; dir is just a filesystem label.
- **Following symlinks into infinite loops.** Use `-L` carefully; break cycles.
- **Reporting every skill individually.** Group first; a flat list of 45 skills is unreadable.
- **Auto-fixing issues.** This skill reports; it does NOT modify. Use `uninstall-skill` for cleanup.
- **Missing project-local skills.** If Mike ran the audit from a project dir, always check `$CWD/.claude/skills/` too.

## Output Format

Always return: (1) summary table, (2) issues list with fix suggestions, (3) full path list ONLY if Mike asks for "--verbose" or "details".
