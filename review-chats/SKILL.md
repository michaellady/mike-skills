---
name: review-chats
description: Use when the user wants to analyze their Claude Code chat history for recurring patterns, forgotten threads, or skill-abstraction candidates ("review my chats", "abstract skills from chat logs", "what am I repeating", "find patterns in my Claude sessions", "retro my chat history").
---

# review-chats

Analyze Mike's Claude Code chat history (~133 project corpuses at `~/.claude/projects/`) to surface recurring workflows that could become skills. Self-bootstrapping — uses parallel Explore subagents to avoid blowing up the main context.

**PAIRED WITH:** When a candidate is selected, chain into `new-skill` to scaffold it.

**REQUIRED BACKGROUND:** Uses `superpowers:dispatching-parallel-agents` pattern — up to 3 Explore agents in parallel, each with a disjoint slice of the corpus.

## When to Use

Use when the user asks:
- "review my chats for patterns"
- "abstract skills from my chat logs"
- "what am I repeating across projects"
- "find skill candidates" / "what should I skill-ify"
- "retro my chat history"

Do NOT use when:
- The user wants to find a specific past conversation (use grep / search directly)
- The user wants today's chat only (too small a corpus)
- The user wants metrics (token counts, durations) — out of scope here

## Corpus Structure

Each chat log is at `~/.claude/projects/<slug>/<session-uuid>.jsonl`. Lines are JSON objects; user messages have `"type":"user"`. The **first user message** of each session is the session-intent signal — most informative per byte.

Total scope (as of 2026-04): ~133 project dirs, mostly small. A handful are large (SignLab-Dev 331MB, superpowers 11MB, etc.) — weight sampling by corpus size.

## Workflow

1. **Scope the corpus.** Default: all of `~/.claude/projects/`. If the user specifies a project filter ("in roxas") or date range ("last month"), narrow accordingly.

2. **Partition the corpus into 3 clusters** for parallel analysis:
   - Cluster A: most recently active (last 2 weeks by mtime)
   - Cluster B: largest by bytes (top 5 dirs by `du -sh`)
   - Cluster C: the rest (sample evenly)

3. **Dispatch 3 Explore subagents in parallel** — one per cluster. Each agent's prompt:
   - Lists the specific project dirs it owns
   - Asks it to extract first-user-messages and look for phrasing patterns
   - Asks it to report in under 400 words: pattern name, frequency, 2-3 verbatim examples, proposed skill scope
   - Explicitly lists the CURRENTLY INSTALLED skills (pass them in) so the agent can filter out already-covered patterns

4. **Merge & dedupe** the three reports. Patterns surfaced by 2+ agents are high-confidence.

5. **Cross-check against installed skills.** Run `ls ~/.claude/skills/ && ls /Users/mikelady/dev/mike-skills/` and compare trigger phrases in candidate descriptions. Drop any candidate whose triggers heavily overlap with an existing skill.

6. **Present candidates** via `AskUserQuestion` (multi-select). Each option shows: pattern name, frequency, one example. Include an "add your own" by letting Mike reply with "Other".

7. **For each selected candidate:** chain into the `new-skill` skill to scaffold it.

## Signal Extraction Heuristics

Pass these hints to the subagents:

| Signal | What it means |
|---|---|
| Same opening phrase across ≥3 sessions (e.g., "install https://...") | Strong skill candidate |
| Repeated tool-use sequences (Bash → Grep → Bash) | Workflow worth abstracting |
| First user message is long and highly structured | Often a template the user re-types |
| Terms like "again", "like last time", "the usual" | User is doing repeat work |
| Sessions ending mid-stream (assistant tool calls, no user response) | Forgotten threads, not patterns |

## Getting the Installed-Skills List

Pass this into every subagent prompt so they can filter:

```bash
( ls ~/.claude/skills/ ; ls /Users/mikelady/dev/mike-skills/ ) | sort -u
```

## Output Format

After all subagents report:

```
Found N candidate patterns (M after filtering against installed skills):

1. <pattern-name> — <frequency>
   Example: "<verbatim user message>"
   Proposed scope: <one sentence>

2. ...
```

Then use `AskUserQuestion` to let Mike select which to build.

## Common Mistakes

- **Running serially instead of in parallel.** The corpus is large; 3 parallel Explore agents cut wall time to ~1/3.
- **Reading full chat logs.** Only the first-user-message of each session is worth the tokens. Grep and sample.
- **Forgetting to filter against installed skills.** Without the filter, the output is 80% things Mike already has.
- **Proposing too many candidates.** Cap at 5-7. More than that paralyzes selection.
- **Missing framework-prefixed skills in the installed-list.** `dialed:*`, `peon-ping-*`, `superpowers:*` count — include them in the filter.
- **Not distinguishing "recurring" from "just happened recently".** A pattern needs to span multiple sessions to be a real candidate.

## Don't Re-Derive

If Mike has already run `review-chats` recently (check `/Users/mikelady/dev/mike-skills/review-chats/last-run.md` if present), start from that baseline rather than re-scanning the entire corpus. Ask Mike if he wants a delta from last run or a fresh full scan.
