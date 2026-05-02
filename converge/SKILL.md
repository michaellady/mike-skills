---
name: converge
description: Use when the user wants Claude and Codex to iterate together until they converge on a mutually-agreed result — or hit a deadlock the user must arbitrate. Works in four modes — plan, implement, verify, review — covering planning, implementation, verification, and code review. Triggers — "converge", "have claude and codex work it out", "iterate with codex until you agree", "claude vs codex on this <plan|code|tests|PR>", "two-AI consensus", "deadlock me a decision".
user_invocable: true
---

# converge

Two-AI iterative refinement across the full development lifecycle. Claude (this agent) and Codex (via the `codex` CLI) take turns critiquing and revising an artifact until either:

1. **Convergence** — both agree the artifact is sound (within a bounded change-rate threshold), OR
2. **Deadlock** — they disagree on a decision neither can resolve from first principles, at which point the user is presented with each side's *best argument* and makes the call.

The four modes share one core convergence loop and differ only in the artifact, the critique prompt, the apply-fixes semantics, and the deliverable.

## Modes

| Mode | Argument | Artifact | Apply-fixes | Deliverable |
|---|---|---|---|---|
| **plan** | `/converge plan` | A plan markdown file (default: most recent in `~/.claude/plans/`) | `Edit` on the plan file | Updated plan + `## CONVERGE LOG` footer |
| **implement** | `/converge implement` | The working tree against a stated goal (typically tied to a plan file) | `Edit`/`Write` on source files, with preview-then-apply on each diff | Working tree at converged state + `CONVERGE-LOG.md` at repo root |
| **verify** | `/converge verify` | Test suites + formal-verification specs + CI config | `Edit`/`Write` on test/spec/CI files | Updated tests/specs/CI + `CONVERGE-LOG.md` |
| **review** | `/converge review` | A diff against a base branch (local branch or PR #) | **No auto-apply** — produces findings only | `REVIEW.md` with cited issues + verdict |

If the user types just `/converge` with no mode, infer from context (active plan file → `plan`; uncommitted changes → `implement` or `review`; otherwise ask) and confirm via AskUserQuestion before proceeding.

## Requirements

- `codex` CLI on PATH (`which codex`). If absent: stop and tell the user to install it (`npm install -g @openai/codex`).
- For modes `implement`, `verify`, `review`: a git repository at the working directory.
- For mode `review`: either uncommitted changes, an active branch with commits ahead of base, or an explicit PR # passed as `/converge review <PR>`.

## Common process

### Step 0 — Mode + scope confirmation

1. Determine mode from the argument or infer it.
2. Identify the artifact:
   - **plan:** plan file path (user-provided > active plan in conversation > most recent project-scoped plan in `~/.claude/plans/`).
   - **implement:** the goal statement (from active plan file or user prompt) + the directory scope (default: project root, or a subdirectory the user names).
   - **verify:** the package(s) under verification + the verification toolchain (tests only, formal verifier, both). Detect from project: `go test`, `cargo test`, Gobra annotations, Verus annotations, etc.
   - **review:** the base branch + diff range. Use `gh pr view --json baseRefName -q .baseRefName` for PRs; `git symbolic-ref refs/remotes/origin/HEAD` else; fall back to `main`.
3. Confirm scope and stop conditions with the user via **one** AskUserQuestion call. Defaults shown — accept them unless changed:
   - **Max rounds:** 5
   - **Convergence threshold:** "both sides return ≤2 substantive issues AND ≥1 explicit agreement signal"
   - **Deadlock surface:** "any single decision where Claude and Codex have re-stated opposing positions across 2 consecutive rounds"
   - **Mode-specific:**
     - `implement` only: "Auto-apply minor edits, pause on major scope changes" (default) or "Preview every edit"
     - `review` only: "Findings only, no auto-fix" (default)
4. Print: `Converging on <artifact>. Mode: <mode>. Up to N rounds. I'll surface deadlocks for you to decide.`

### Step 1 — Establish the LOG location

| Mode | Log file |
|---|---|
| plan | Append `## CONVERGE LOG` section to the plan file |
| implement | Create/append `CONVERGE-LOG.md` at the repo root |
| verify | Create/append `CONVERGE-LOG.md` at the repo root |
| review | Output goes into `REVIEW.md` at the repo root |

If a log section already exists from a prior run, append a new dated subsection (`### Run YYYY-MM-DD HH:MM`) rather than overwriting.

Initial log table header:

```markdown
## CONVERGE LOG

| Round | Author | Verdict | Issues raised | Issues conceded |
|-------|--------|---------|---------------|-----------------|
```

### Step 2 — Round loop

For round `r` from 1 to N:

#### 2a. Claude critique pass

Read the current artifact state, produce a structured critique with this exact JSON shape (saved to `/tmp/converge-claude-r{r}.json` and shown in conversation as a fenced ```json block):

```json
{
  "round": 1,
  "author": "claude",
  "mode": "plan|implement|verify|review",
  "verdict": "needs_revision | converged",
  "issues": [
    {"id": "C1", "severity": "critical|major|minor",
     "claim": "<one-sentence problem>",
     "rationale": "<why it's a problem>",
     "proposed_fix": "<concrete edit / finding>",
     "evidence": "<file:line or quote — required for implement/verify/review modes>"}
  ],
  "concessions": [
    {"on": "<previous codex issue id>", "reason": "<why claude now agrees>"}
  ],
  "open_disagreements": [
    {"with": "<codex issue id>",
     "claude_position": "<one sentence>",
     "codex_position": "<one sentence>",
     "stuck_reason": "<why first principles cannot resolve this>"}
  ]
}
```

Mode-specific critique focus:

| Mode | What Claude critiques |
|---|---|
| **plan** | Logical gaps, unstated assumptions, missing edge cases, overcomplexity, feasibility risk, missing dependencies |
| **implement** | Plan/code drift, untested branches, error handling, naming, dead code, pattern inconsistency vs the codebase, missing fixtures |
| **verify** | Coverage gaps (real coverage, not a manifest grep), missing property tests, missing negative tests, weak invariants, unverified preconditions, race conditions, CI gate adequacy |
| **review** | Pre-landing concerns: SQL injection, trust-boundary violations, conditional side effects, breaking changes, unsafe migrations, PR scope creep, missing tests for new behavior |

Rules for every Claude pass:
- Cap `issues` at 5 per round. If more emerge, keep highest-severity 5 and note the rest are deferred.
- An issue is "substantive" if `severity` is `critical` or `major`. Minor issues do not block convergence.
- Each `proposed_fix` must be concrete and testable, not "consider X."
- For `implement`/`verify`/`review` modes, every issue MUST include `evidence` — file:line or a quoted code/spec snippet. No evidence ⇒ drop the issue.
- If zero substantive issues remain AND no open disagreements, set `verdict` to `converged`.

#### 2b. Apply Claude's proposed fixes (skip in `review` mode)

For each issue in 2a where Claude proposes a fix:
- **plan mode:** edit the plan file via `Edit`.
- **implement mode:** edit source files via `Edit`/`Write`. **If the user chose "Preview every edit" in Step 0, show the diff and pause for confirmation. Otherwise auto-apply minor edits, but pause on major scope changes** (file deletions, new dependencies, public API renames).
- **verify mode:** edit test/spec/CI files via `Edit`/`Write`. Run the verifier/test suite after each batch of edits and capture pass/fail in the LOG.
- **review mode:** **do not apply fixes.** Append the issue to the in-progress `REVIEW.md` instead.

For implement/verify modes, after applying fixes, run a smoke check appropriate to the mode:
- `implement`: build succeeds (`go build ./...` / `cargo check` / `tsc --noEmit` etc., detected from project)
- `verify`: tests pass and verifier (if any) discharges all obligations
If the smoke check fails, **revert the round's edits** and surface the failure to the user before proceeding.

#### 2c. Codex critique pass

Build a prompt that includes:
- The current (just-edited) artifact state — for `implement`/`verify`/`review`, include the relevant `git diff` since the round started; for `plan`, include the full plan file
- Claude's just-emitted JSON critique
- The full prior LOG table (so codex sees the trajectory)
- Mode-specific critique focus (same table as 2a)
- Instruction: "Respond with the same JSON shape, your role is `codex`, issue ids are `K1, K2, …`. Use `concessions` to acknowledge any of Claude's points you now agree with. Use `open_disagreements` for anything you and Claude have re-stated across rounds without converging. Every issue must include `evidence`."

Run codex with JSONL output:

```bash
codex exec --skip-git-repo-check "<prompt>" -s read-only \
  -c 'model_reasoning_effort="xhigh"' \
  --enable web_search_cached --json 2>/dev/null \
  | <JSONL parser from /codex skill>
```

Use `timeout: 300000` (5 min) per call. If codex times out, treat that round as deadlocked-by-timeout — surface to user.

For `review` mode specifically, codex runs against the diff: prepend the prompt with `git diff <base>...HEAD` output (truncated to 50KB) and instruct codex to focus on landing risk.

Save Codex's response to `/tmp/converge-codex-r{r}.json`.

#### 2d. Apply Codex's proposed fixes

Same edit rules as 2b. Skip in `review` mode (findings only).

#### 2e. Update CONVERGE LOG

Append two rows to the table — one per pass:

```markdown
| 1 | claude | needs_revision | C1, C2, C3 | (none) |
| 1 | codex  | needs_revision | K1, K2     | C1     |
```

For `implement`/`verify`, also append a `Smoke check: pass|fail (cmd: <command>)` line after each pass that ran one.

#### 2f. Check stop conditions

- **Convergence:** both `verdict == "converged"` in the same round → exit loop, go to Step 3.
- **Soft convergence:** both verdicts are `needs_revision` but the union of substantive `issues` across both passes ≤ 2 AND `open_disagreements` is empty → ask the user via AskUserQuestion whether to call it converged or run one more round. Default: converge.
- **Deadlock:** the same `open_disagreements` entry (matched by `claude_position` + `codex_position` similarity, or by repeated issue ids referenced as "still disputing K3") appears in 2 consecutive rounds → exit loop, go to Step 4.
- **Max rounds:** if `r == N` and not converged, exit loop, go to Step 4 with the unresolved disagreements.
- **Smoke check failed twice in a row** (implement/verify only): exit loop, go to Step 4 — the artifact is not stable enough for further iteration without user input.

### Step 3 — Convergence path

Print:

```
✅ Claude and Codex converged on <artifact>. Mode: <mode>. R rounds.

Final state: <one-paragraph summary of what changed from the original>
Issues resolved: <count>
Concessions made: claude=N, codex=M
Smoke checks: <pass count>/<total run> (implement/verify only)
```

Then write a final `### Converged Result Summary` subsection to the LOG capturing:
- **plan:** original plan one-liner → final plan one-liner; 3-5 most consequential changes
- **implement:** files changed, LOC delta, plan-coverage assessment ("plan F1-F4 implemented; F5 deferred")
- **verify:** coverage delta, new property tests added, verifier-obligation count delta
- **review:** N/A (review mode goes through Step 4 always — see below)

For `review` mode, "convergence" means both reviewers signed off with verdict `converged`. Write `REVIEW.md` with:

```markdown
# Review of <branch> against <base>

**Verdict:** APPROVED — both Claude and Codex signed off after R rounds.

## Findings (resolved during review)
<all issues raised across rounds, marked resolved>

## Reviewer notes
<any minor non-blocking observations>
```

Stop.

### Step 4 — Deadlock / unresolved path

For each unresolved `open_disagreement`, present the user a structured arbitration block. Use AskUserQuestion **one decision at a time** (per gstack AskUserQuestion conventions), with this exact preview structure:

```
DEADLOCK #N — <one-line subject>

Mode: <mode>
Affected: <file path / plan section / test name>

Claude's position:
  <claude_position>
  Best argument: <claude's strongest single sentence for their side>
  What it costs if codex is right: <one sentence>

Codex's position:
  <codex_position>
  Best argument: <codex's strongest single sentence for their side>
  What it costs if claude is right: <one sentence>

Recommendation: <if one side has a 70/30 lean, name it; else "genuine taste call — pick the one that fits your priorities">
```

To get each side's "best argument," do a final adversarial pass:
1. Internally: "You are committed to your position on DEADLOCK #N. Codex disagrees. Write your single strongest sentence and the cost of being wrong. Do not hedge."
2. Send Codex an analogous prompt via `codex exec`.

Options:
- A) Take Claude's side
- B) Take Codex's side
- C) Hybrid (user describes how to reconcile)
- D) Defer (mark unresolved in LOG, move on)

Apply the user's choice to the artifact (skip apply for `review` mode — record the decision in `REVIEW.md`). Append the user's decision + rationale to the LOG with author=`user`.

After all deadlocks are arbitrated:

```
🤝 Reached negotiated agreement on <artifact>. Mode: <mode>. R rounds.
Convergence: partial — <D> deadlocks resolved by user, <C> auto-converged items.
Log: <log path>
```

For `review` mode, the final `REVIEW.md` records each deadlock + the user's verdict and includes a `## User decisions` section.

### Step 5 — Cleanup

```bash
rm -f /tmp/converge-claude-r*.json /tmp/converge-codex-r*.json
```

Do NOT delete the LOG / REVIEW files — those are the deliverable.

## Mode-specific guidance

### plan mode

- Most analogous to `/codex` consult mode but iterative. Use this when a plan needs more than one critique round.
- The plan file IS the working state. Every accepted edit is applied in place.
- Convergence here is cheap (no smoke checks). Soft-convergence usually triggers in 2-3 rounds.

### implement mode

- Treat the active plan file (if any) as the contract. Convergence ≠ "code is perfect" — it's "code matches the plan and both reasoners see no remaining substantive issues."
- Auto-apply only when the edit is a single-function change with no API surface impact. Anything that touches public types, public functions, or dependency files (`go.mod`, `Cargo.toml`, `package.json`) requires preview confirmation.
- Run the smoke check (build + tests) every round. Two consecutive smoke-check failures exit to deadlock.
- Pair this with `/converge verify` afterward — implement convergence does not guarantee adequate test coverage.

### verify mode

- Critique focuses on what's *not* tested or proved, not on whether existing tests pass.
- Treats the verifier toolchain as an oracle: if Gobra/Verus/cargo-test runs in the project, run it after every fix-application round and use the obligation/coverage delta as ground truth.
- Coverage threshold is taken from the project's CI config if available; otherwise prompt the user.
- Both reasoners are explicitly told that "100% line coverage" is a weak signal alone — they must propose property tests, edge cases, and adversarial inputs, not just trivial branch tests.

### review mode

- Read-only with respect to the working tree. The deliverable is `REVIEW.md`.
- Round 1 is each reasoner's independent diff review. Subsequent rounds are them critiquing each other's findings — letting one side overrule the other when its evidence is stronger.
- A finding survives to `REVIEW.md` only if it has explicit evidence (file:line) and was not conceded by the originator in a later round.
- Output verdict is `APPROVED` (both converged with no critical findings), `APPROVED_WITH_NITS` (only minor findings remain), `CHANGES_REQUESTED` (≥1 critical/major finding), or `BLOCKED` (deadlock the user must arbitrate).

## Failure modes & edge cases

- **Codex auth error:** stop, tell user to run `codex login`. Artifact is left at whichever round-state it reached; LOG records the partial run.
- **Plan file or diff is huge (>50KB):** warn the user; codex exec input has practical limits. Offer to converge on a subset (one section, one package, one file).
- **Both sides converge in round 1:** still write the LOG with the single round; this is a successful no-op review.
- **Same issue keeps reappearing:** that's a deadlock — surface it.
- **Malformed JSON from one side:** retry once with a stricter prompt; if still malformed, treat that side's verdict as `converged` for that round and let the other side's critique drive — note in LOG.
- **User interrupts mid-round:** the artifact is always in a consistent post-edit (and post-smoke-check, for implement/verify) state at round boundaries. Resume with `/converge <mode>` — the LOG tells you where you left off.
- **Implement mode breaks the build twice:** stop, surface the last passing state and the failing diff. Don't keep iterating on a broken tree.
- **Review mode and the diff is empty:** no work to do; tell the user.

## What this skill is NOT

- Not a way to launder a bad artifact into a "verified" one. Two AIs agreeing means they don't see further problems within their training, not that the artifact is correct.
- Not a substitute for `/plan-eng-review`, `/plan-ceo-review`, or human review when stakes are high.
- Not for one-shot reviews — if you only want a single critique, use `/codex review` (diff) or `/codex` consult (plan).
- Not an autonomous coder — implement mode pauses on scope-shifting edits and bails on broken builds.

## Example transcript shapes (abbreviated)

### plan mode — convergence
```
> /converge plan ~/.claude/plans/build-twitter-clone.md

Mode: plan. Up to 5 rounds.

ROUND 1
[claude: 4 issues C1-C4, verdict needs_revision]
Applied C1, C2, C4 to plan file.
[codex: 3 issues K1-K3, conceded C1, verdict needs_revision]
Applied K1, K3 to plan file.

ROUND 2
[claude: 1 issue C5, conceded K3, verdict needs_revision]
[codex: 0 issues, verdict converged]

ROUND 3
[claude: 0 issues, verdict converged]

✅ Converged after 3 rounds. 7 issues resolved.
```

### implement mode — smoke-check fail
```
> /converge implement

Mode: implement. Goal: implement F1-F4 from plan. Up to 5 rounds.

ROUND 1
[claude: 3 issues — missing tests for unfollow, no Mutex on store, no error path on PostTweet]
Applied fixes to internal/store/memstore.go, internal/service/service.go.
Smoke check: PASS (go build ./... && go test ./...)
[codex: 2 issues — race on follow set, missing F4 self-follow rejection]
Applied fixes.
Smoke check: FAIL (TestFollow_Self: expected 400, got 204)

Reverting round 2 edits.
DEADLOCK — codex's proposed fix breaks the test it added. User input needed.
```

### review mode — verdict
```
> /converge review 1234

Mode: review. PR #1234 against main. Up to 5 rounds.

ROUND 1
[claude: 2 findings — F1 (untested error path), F2 (deprecated API call)]
[codex: 3 findings — K1 (SQL injection at handlers.go:42), K2 (race in cache.go:88), K3 (style nit)]

ROUND 2
[claude: conceded F2 (codex showed deprecation path was guarded), confirms K1 and K2]
[codex: conceded K3 (nit), upgrades K1 to critical]

✅ APPROVED_WITH_CHANGES — wrote REVIEW.md with K1, K2 (critical), F1 (major).
```
