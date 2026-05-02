---
name: primitive-test
description: Use when designing a skill, agent, or system and you have to decide whether a capability belongs in code (a script/helper) or in the prompt (the SKILL.md / agent instructions / model reasoning). Applies the three-condition Primitive Test — Atomicity, Bitter Lesson, ZFC — and produces a per-capability verdict. Triggers — "primitive test", "should this be in code or in the prompt", "encode or leave in prompt", "what should this script do vs the SKILL.md", "audit my skill for over-prompting / under-encoding", "is this judgment or transport".
user_invocable: true
---

# primitive-test

A decision framework for splitting a skill cleanly into **code** (scripts you call) and **prompt** (instructions in the SKILL.md). Three conditions; a capability belongs in code **only if all three pass.**

The full framework lives in [`REFERENCE.md`](./REFERENCE.md). Read that first if you haven't seen it before. This SKILL.md is the operational wrapper — how to apply it on demand.

## When to invoke this skill

- You're authoring a new skill and the SKILL.md is filling up with bash one-liners and project-type detection.
- You're reviewing a skill and want to know which pieces should be extracted to scripts.
- You're about to write `if X then Y` logic in code and aren't sure if it's transport or judgment.
- The user asks: "does this skill pass the primitive test?" or "what should be in code vs the prompt?"

## The three conditions (one-liner each)

1. **Atomicity** — Could two concurrent invocations corrupt state? If yes and the underlying tool doesn't already protect, code must.
2. **Bitter Lesson** — Imagine a 10× smarter model. Does this capability become *less* needed (→ prompt) or *exactly as* needed (→ code)?
3. **ZFC** — Does the implementation contain any judgment call (`if stuck then X`)? Yes → cognition → prompt. No → transport → code.

A capability that fails any one condition belongs in the prompt.

## How to run the test

### Step 1 — List candidate capabilities

Enumerate every "thing this skill does" at the granularity of one verb. For an existing skill, scan SKILL.md for:

- Bash blocks
- Decision rules ("if X happens, do Y")
- File-format / log-format details
- Tool invocation recipes (timeouts, retry, parsing)
- "Detect…" / "Choose…" / "Decide…" sentences

For a new skill, list everything you imagine the skill needing to do.

### Step 2 — Score each capability against the three conditions

Use this table. Be specific in the "why" cells — vague answers ("it's complicated") usually mean you should re-split the capability.

| # | Capability | Atomicity (Y/N — why) | Bitter Lesson (pass/fail — why) | ZFC (pass/fail — why) | Verdict |
|---|---|---|---|---|---|
| 1 | ... | ... | ... | ... | code / prompt |

### Step 3 — Apply the verdicts

| Verdict | What to do |
|---|---|
| All three pass | Extract to a script in `<skill>/scripts/`. The SKILL.md should call it, not re-derive it. |
| Atomicity fails | Either build the atomic version in code, or — if the underlying tool *should* be atomic — file an upstream bug instead of wrapping. |
| Bitter Lesson fails | Leave it in the prompt. A smarter model will do this better next year. |
| ZFC fails | Leave it in the prompt. Judgment doesn't compress into a script. |

### Step 4 — Sanity-check the split

Two failure modes to look for after the cut:

- **Over-encoded:** A script that takes 8 flags and contains `case` statements selecting between strategies — that's smuggled judgment. Split into multiple narrow scripts and let the prompt pick.
- **Under-encoded:** A SKILL.md that re-derives `gh pr view --json baseRefName ... fall back to main` every run — that's transport in prose. Encode it.

## Quick examples (skill design)

| Capability | A | BL | ZFC | Verdict |
|---|---|---|---|---|
| "Run codex exec, parse JSONL, return final message" | n/a | pass | pass | **code** |
| "Decide whether codex's critique is stronger than claude's" | n/a | fail | fail | **prompt** |
| "Detect project build command from `go.mod` / `Cargo.toml`" | n/a | pass | pass | **code** |
| "Choose between auto-apply and preview-then-apply for an edit" | n/a | fail | fail | **prompt** |
| "Append a row to a CONVERGE LOG markdown table" | naturally append-only | pass | pass | **code** |
| "Decide when 'soft convergence' applies" | n/a | fail | fail | **prompt** |
| "Clone a GitHub repo into a TTL-bounded local cache" | git is atomic; FS rename is atomic | pass | pass | **code** |
| "Decide whether to clone vs hit the API once" | n/a | fail | fail | **prompt** |

## Output format

When asked to apply this skill to a target (a skill name, a directory, a paste of a SKILL.md), produce:

1. **Capabilities table** (Step 2) — every candidate with all three condition scores.
2. **Verdict summary** — counts of code / prompt / fix-upstream.
3. **Concrete recommendations** — for each "code" verdict, the script name and one-line interface (`scripts/<name>.sh <args>` → `<output>`); for each "prompt" verdict, the section of the SKILL.md it belongs in.
4. **Risks** — over-encoded scripts (smuggled judgment) and under-encoded prompts (transport in prose) you spotted.

Keep it tight: a real audit fits in well under a page.

## What this skill is NOT

- Not a license to encode everything that *can* be encoded. If a capability passes ZFC + Bitter Lesson but only runs once per skill invocation and is two lines, leaving it in the SKILL.md is fine — the test tells you what *can* be code, not what *must* be.
- Not a substitute for skill review. It catches structural splits, not content quality.
- Not specific to Claude Code skills — works for any "agent prompt + helper code" system.
