---
name: new-skill
description: Use when the user wants to scaffold a new Claude Code skill ("new skill", "scaffold a skill", "create a skill for X", "start a skill called Y", "bootstrap a skill").
---

# new-skill

Scaffold a new Claude Code skill inside Mike's authoring workspace at `/Users/mikelady/dev/mike-skills/`, then print the one-liner that installs it globally.

**REQUIRED BACKGROUND:** You MUST follow `superpowers:writing-skills`. This skill handles mechanics only — it does NOT replace the TDD discipline for authoring the skill's content.

## When to Use

Use when Mike says:
- "new skill for X"
- "scaffold a skill that does Y"
- "create a skill: Z"
- "start a skill called ..."

Do NOT use when:
- Mike is editing an existing skill (just edit the file)
- Mike wants to install an external framework (use `install-skill-framework`)
- The work could be a hook or CLAUDE.md entry instead (ask first)

## Workflow

1. **Collect two inputs from Mike:**
   - `name` — lowercase, hyphens only, no collisions (see step 2)
   - `purpose` — one sentence, becomes the seed for the description

2. **Validate name.** Must match `^[a-z][a-z0-9-]*$`. Check for collisions:
   - `ls ~/.claude/skills/` (global)
   - `ls /Users/mikelady/dev/mike-skills/` (workspace)
   - Any framework-prefixed names (`dialed:*`, `peon-ping-*`, `superpowers:*`, `dialed:*`)

   If a collision, stop and ask Mike to rename.

3. **Create the directory:**
   ```bash
   mkdir -p /Users/mikelady/dev/mike-skills/<name>
   ```

4. **Write SKILL.md** using the template below. Fill in `name`, convert `purpose` into a proper "Use when..." description, and leave `## Workflow` as TODO stubs for Mike to fill in.

5. **Print the install one-liner:**
   ```
   ln -s /Users/mikelady/dev/mike-skills/<name> ~/.claude/skills/<name>
   ```

6. **Remind Mike of the TDD discipline:** run a baseline test with a subagent BEFORE writing content (per `superpowers:writing-skills`).

## Template

```markdown
---
name: <name>
description: Use when <specific triggering conditions>. <one-sentence value>.
---

# <name>

<One-paragraph overview. What this skill does, in plain language.>

## When to Use

Use when:
- <trigger phrase>
- <trigger phrase>

Do NOT use when:
- <negative case>

## Workflow

1. TODO
2. TODO
3. TODO

## Common Mistakes

- TODO
```

## Frontmatter Rules (Enforced)

- `name` matches `^[a-z][a-z0-9-]*$`
- `description` starts with "Use when"
- `description` describes *triggering conditions*, NOT the workflow (per `superpowers:writing-skills` CSO section)
- Full frontmatter under 1024 chars

## Optional Subdirectories

Only create when the skill actually needs them:
- `references/` — large reference docs (100+ lines, e.g. API specs)
- `examples/` — runnable example scripts
- `scripts/` — reusable helper scripts

Do NOT scaffold empty `references/` and `examples/` dirs by default — empty dirs are noise.

## Common Mistakes

- **Summarizing the workflow in `description`.** Claude follows the description shortcut instead of reading the body. Keep description to triggers only.
- **Creating empty `references/`/`examples/` dirs.** Only add when needed.
- **Skipping collision check.** Silent overwrites are painful to reverse.
- **Skipping the baseline test.** Writing a skill without seeing the RED failure first violates `superpowers:writing-skills` Iron Law.
- **Installing globally before testing.** The symlink one-liner is the LAST step, after the skill has been iterated on in the workspace.

## Install One-Liner

After the skill is tested and ready:

```bash
ln -s /Users/mikelady/dev/mike-skills/<name> ~/.claude/skills/<name>
```

Symlinking (not copying) means edits in the workspace take effect immediately in all Claude Code sessions.
