---
name: resume-broken-run
description: Use when an agent, workflow, or fan-out died mid-work — API connection drops, session/spend limits, forced structured-output timeouts — and work must be recovered without loss. Triggers — "resume the run", "the agent died", "hit the session limit", "recover the workflow", "connection closed mid-response".
user_invocable: true
---

# resume-broken-run

A dead agent almost never means dead work. Agents commit as they go, worktrees persist, and workflow runs cache completed lanes — the loss is usually just the tail of one lane, and even that is sitting uncommitted on disk. The failure mode this skill kills is the reflexive "start over": re-running finished lanes, clobbering a branch that already had the work, or resuming under the same condition that killed the run. Recover in this order.

## 1. Inspect before assuming loss

Find the dead agent's worktree: `git worktree list`. Workflow worktrees live at `.claude/worktrees/<runId>-N`. In each suspect worktree, check `git status` and `git log` on the lane branch — dead agents usually leave **committed progress plus uncommitted WIP**. Read what's actually there before deciding anything was lost.

## 2. Salvage

- Commit the uncommitted WIP on the lane branch as:
  `wip(<scope>): <lane> — agent died mid-work; continuing agent must review and finish`
  (with the standard trailers). The message is a contract: it tells the next agent this commit is unreviewed and possibly incomplete.
- **Never rebase or rewrite the branch.** The history is the recovery record.
- Then `git worktree remove` the orphan so the branch is checkout-able elsewhere. Use `--force` only for cache-dir dirt (`.gocache/` etc.) — never to discard real changes you haven't committed.

## 3. Resume paths, in order of fidelity

**(a) A direct Agent that stopped** → `SendMessage` to its agentId. Context intact — it picks up mid-thought. This works repeatedly across multiple crashes of the same agent; keep messaging the same id.

**(b) A Workflow run** → edit ONLY the errored agents' prompts in the persisted script, then relaunch with `{scriptPath, resumeFromRunId}`. The sharp edge: **completed agents' (prompt, opts) pairs must stay byte-identical or their cache busts** and they re-run from scratch. When a shared prompt-builder must gain new text for the failed lanes, key an exclusion set on the exact completed `(name|branch)` combos so the builder emits the old bytes for them and the new text only for the retries.

**(c) Unrecoverable** (agent gone, no resumable run) → a fresh agent in **continuation mode**. The prompt must say: "the branch ALREADY EXISTS with a wip commit; check it out, do NOT create or reset it; review the WIP against the spec, keep what is right, finish." Without that framing a fresh agent will branch from main and silently orphan the salvage.

## 4. Known causes and their gates

- **Plan mode propagates into subagents.** Build runs launched under plan mode return read-only specs, not code. ExitPlanMode first, then a **FRESH** run — never resume, because the cached results are the read-only ones.
- **Session/spend limits name their reset time.** Read it and schedule the resume after it; resuming before the reset just dies again at the same wall.
- **Parallel Go gates poison the shared build cache** (`make verify` runs `go clean -cache`). Every resumed or concurrent gate runs with `GOCACHE="$PWD/.gocache"` so lanes can't clean each other's caches mid-build.

## 5. After recovery

The continuing work still goes through the independent-verify loop — **a salvaged branch is unreviewed by definition.** The wip commit was written by an agent that died mid-thought; a separate agent re-runs the gate and audits before the lane counts as done.
