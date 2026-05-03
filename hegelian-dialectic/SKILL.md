---
name: hegelian-dialectic
description: Use when you want Claude and Codex to work an artifact through an explicit thesis → antithesis → synthesis loop until a transcendent position emerges (or the dialectic stalls). Works in four modes — plan, implement, verify, review — same artifact types as /converge but with a structurally different rhythm. Triggers — "hegelian dialectic", "thesis antithesis synthesis", "dialectic this <plan|code|tests|PR>", "synthesize claude and codex on this", "transcend the disagreement".
user_invocable: true
---

# hegelian-dialectic

Three-stage iterative refinement across the development lifecycle. Each round runs an explicit **thesis → antithesis → synthesis** cycle. Claude (this agent) states a position, Codex (via `codex` CLI) constructs the strongest opposition, then both propose syntheses and Claude (orchestrator) picks/merges. The synthesis is *applied* to the artifact and becomes the next round's thesis.

The loop ends when:
1. **Convergence** — the antithesis cannot construct a substantive opposition to the latest synthesis, OR
2. **Stalled synthesis** — synthesis is structurally identical to the thesis it tested (no movement), OR
3. **Smoke-check failure repeat** (implement/verify) — two consecutive build/test failures, OR
4. **Max rounds** — 3 cycles completed without convergence; remaining tension surfaced for user arbitration.

This is the sibling skill to `/converge`. Same artifact types, different rhythm: converge is two parallel critiques per round; hegelian-dialectic is a three-stage sequence per round with synthesis as a deliverable in its own right.

## Modes

| Mode | Argument | Artifact | Apply step | Deliverable |
|---|---|---|---|---|
| **plan** | `/hegelian-dialectic plan` | A plan markdown file (default: most recent in `~/.claude/plans/`) | `Edit` on the plan file per round's synthesis | Updated plan + `## HEGELIAN LOG` footer |
| **implement** | `/hegelian-dialectic implement` | The working tree against a stated goal | `Edit`/`Write` per synthesis, with preview-then-apply on scope changes | Working tree at synthesized state + `HEGELIAN-LOG.md` at repo root |
| **verify** | `/hegelian-dialectic verify` | Tests + verifier specs + CI config | `Edit`/`Write` per synthesis | Updated tests/specs/CI + `HEGELIAN-LOG.md` |
| **review** | `/hegelian-dialectic review` | A diff against a base branch (local or PR #) | **No working-tree edits.** Synthesis is written to `HEGELIAN-REVIEW.md` | `HEGELIAN-REVIEW.md` with verdict + cited issues |

If the user types just `/hegelian-dialectic` with no mode, infer from context (active plan file → `plan`; uncommitted changes → `implement` or `review`; otherwise ask) and confirm via AskUserQuestion before proceeding.

## Requirements

- `codex` CLI on PATH (`which codex`). If absent: stop and tell the user to install it (`npm install -g @openai/codex`).
- The transport binary at `/Users/mikelady/dev/mike-skills/converge/bin/converge` (built from the `converge` skill's Go source). If missing, run `bash /Users/mikelady/dev/mike-skills/converge/build.sh` — needs Go 1.25+, no external deps.
- For modes `implement`, `verify`, `review`: a git repository at the working directory.
- For mode `review`: either uncommitted changes, an active branch with commits ahead of base, or an explicit PR # passed as `/hegelian-dialectic review <PR>`.

## Transport binary (reuse converge's binary, do not re-build)

All transport work — codex invocation, diff retrieval, log formatting, schema validation, smoke checks, status snapshots — delegates to the existing `bin/converge` binary in the sibling `converge` skill. **No new binary, no new Go code.** Set this once at the top of the run:

```bash
HEG_BIN="/Users/mikelady/dev/mike-skills/converge/bin/converge"
```

Subcommands used (all mode-agnostic at the transport layer):

| Subcommand | Purpose |
|---|---|
| `$HEG_BIN preflight <mode>` | Verify codex on PATH + authenticated, git repo (where required), gh CLI present (review). Step 0. |
| `$HEG_BIN resolve-plan [path]` | Resolve plan file (explicit > `$CONVERGE_ACTIVE_PLAN` > repo-slug match in `~/.claude/plans/` > most-recent). |
| `$HEG_BIN detect-base-branch [pr#]` | `gh pr view` → `gh repo view` → `origin/HEAD` → `origin/main`/`master`. |
| `$HEG_BIN get-diff <base> [pr#]` | `git diff base...HEAD` or `gh pr diff`, truncated to 50KB. |
| `$HEG_BIN codex-critique [--resume <id>] [--model <m>] <prompt-file> [effort]` | Run `codex exec`. Streams events to stderr. Stdout = final assistant message only. Round 1 starts a new thread, captures the id at `$CONVERGE_THREAD_OUT` (default `/tmp/converge-thread-<pid>.txt`). Subsequent calls within the same round use `--resume <id>`. |
| `$HEG_BIN claude-critique [--resume <id>] [--model <m>] <prompt-file> [effort]` | Same shape, routed through `claude -p ... --output-format stream-json`. Captures the session UUID for resume. Use this when the user picks claude as the antithesis-author. |
| `$HEG_BIN llm-critique --provider {codex\|claude} [--resume <id>] [--model <m>] <prompt-file> [effort]` | Generic form — pick provider explicitly. The two `*-critique` subcommands are aliases. |
| `$HEG_BIN validate-critique <json>` | Validate against the embedded JSON Schema. Set `CONVERGE_REQUIRE_EVIDENCE=1` for implement/verify/review. |
| `$HEG_BIN smoke-check build\|test` | Project-type detection and run. |
| `$HEG_BIN log {init\|row\|smoke\|note} <file> ...` | LOG / REVIEW writer (write `HEGELIAN-LOG.md` / `HEGELIAN-REVIEW.md` to avoid clobbering converge's files). |
| `$HEG_BIN status {start\|round\|thread\|verdict\|end\|path\|show} <session-id> ...` | Per-round status snapshot. Use `$$` (your prompt's PID) as the session id. |
| `$HEG_BIN cleanup` | Remove `/tmp/converge-*` per-round payloads. |

When you invoke `codex-critique`, **leave its stderr connected to your terminal** so the user sees the heartbeat.

**Schema constraint:** The critique schema has `additionalProperties: false`. Both antithesis and synthesis JSON outputs reuse the existing schema as-is — no new fields. The synthesis position is carried in `summary` (one paragraph) plus `next_steps[]` (the integrated change list); concessions go in `concessions[]`; remaining tensions go in `issues[]` (which should be empty if synthesis is fully transcendent).

**Antithesis-author choice (codex vs claude):** the antithesis-author defaults to **codex** (matches the templates' tone and effort settings). To use claude as the antithesis-author instead, swap `codex-critique` for `claude-critique` in the Phase B / Phase C calls below. The hegelian-dialectic prompt templates use `{{AUTHOR}}` and `{{REVIEWER_NAME}}`-equivalent neutral framing, so they work for either provider without edits — just pass `AUTHOR=claude` (or `AUTHOR=codex`) to the placeholder substitution. Ask the user in Step 0 if they want to override the default. The schema's `author` enum already accepts both values.

## Per-round structure

Each round runs **four phases** before checking stop conditions: Thesis → Antithesis → Synthesis (Codex + Claude candidates) → Pick/Apply.

### Phase A — Thesis (Claude, in-context, no codex call)

Compose or capture the position to be tested:

| Mode | Round 1 thesis | Round N+1 thesis |
|---|---|---|
| **plan** | The plan file as-is | The plan file as edited by the previous round's synthesis |
| **implement** | The working tree against the goal | The post-synthesis working tree |
| **verify** | Current tests/specs | The post-synthesis tests/specs |
| **review** | Claude writes its own review of the diff (in JSON form, validated against the schema) | The previous round's synthesized review |

Save the thesis text/state to `/tmp/hegel-thesis-$$-r{r}.md` (for review mode, save the JSON to `/tmp/hegel-thesis-$$-r{r}.json` and validate it):

```bash
CONVERGE_REQUIRE_EVIDENCE=1 $HEG_BIN validate-critique /tmp/hegel-thesis-$$-r{r}.json
# (review mode only; other modes capture the artifact, not a critique)
```

For modes other than `review`, the thesis is the artifact itself — no JSON. Show the thesis in conversation as a fenced block (truncate to ~50 lines for readability if longer).

### Phase B — Antithesis (Codex)

Render the antithesis prompt for the mode. Templates live in `prompts/antithesis-{plan,implement,verify,review}.tmpl`. Read the template, substitute placeholders inline, and write to `/tmp/hegel-antithesis-prompt-$$-r{r}.txt`. Placeholders:

- `{{ROUND}}` / `{{MAX_ROUNDS}}` — current/total round count
- `{{IF_RESUME}}...{{ENDIF_RESUME}}` — keep the inner text iff round > 1, otherwise strip
- `{{ARTIFACT}}` — full artifact for plan mode; diff for implement/review; tests+spec list for verify
- `{{THESIS}}` — the thesis text (review mode: the thesis JSON)
- `{{PRIOR_LOG}}` — tail of the LOG table (rounds 1..r-1)
- `{{BASE_BRANCH}}` — review mode only

Then call codex:

```bash
# Round 1 (fresh thread):
$HEG_BIN codex-critique /tmp/hegel-antithesis-prompt-$$-r{r}.txt > /tmp/hegel-antithesis-$$-r{r}.json
THREAD_ID=$(cat "${CONVERGE_THREAD_OUT:-/tmp/converge-thread-$$.txt}")
$HEG_BIN status thread "$$" "$THREAD_ID"

# Round 2..N (resume the same thread):
$HEG_BIN codex-critique --resume "$THREAD_ID" /tmp/hegel-antithesis-prompt-$$-r{r}.txt > /tmp/hegel-antithesis-$$-r{r}.json
```

Validate:

```bash
CONVERGE_REQUIRE_EVIDENCE=1 $HEG_BIN validate-critique /tmp/hegel-antithesis-$$-r{r}.json
# (omit CONVERGE_REQUIRE_EVIDENCE for plan mode)
```

The antithesis JSON has `author=codex`, `mode=<mode>`, `verdict` ∈ {`needs_revision`, `converged`}. **Verdict `converged` here means "I cannot construct a substantive opposing position."** Issue ids are K1..K5.

If codex exits 3 (auth) → stop, tell user to run `codex login`. Exit 4 (timeout) → treat as failed antithesis, surface to user. Exit 5 (no message) → retry once with stricter "JSON only" suffix on the prompt; if still empty, treat antithesis verdict as `converged` for this round and proceed to synthesis with what we have.

### Phase C — Synthesis (parallel candidates)

Two synthesis candidates are produced. Both use the same `prompts/synthesis-<mode>.tmpl` template, fed thesis + antithesis as inputs. The template instructs the producer to:

- Identify what survives the antithesis (what should be preserved from the thesis)
- Identify what the antithesis got right (what must change)
- Produce an integrated position that transcends both, framed as a one-paragraph `summary` + an action list in `next_steps[]`
- List concessions in `concessions[]` (one per side that gave ground)
- List remaining tensions (if any) in `issues[]` — should be near-empty for a strong synthesis
- Set `verdict=converged` if the synthesis fully resolves the tension; `needs_revision` if substantial disagreement remains

**Codex synthesis candidate:**

```bash
# Render synthesis prompt, then resume same codex thread:
$HEG_BIN codex-critique --resume "$THREAD_ID" /tmp/hegel-synthesis-prompt-$$-r{r}.txt > /tmp/hegel-synthesis-codex-$$-r{r}.json
CONVERGE_REQUIRE_EVIDENCE=1 $HEG_BIN validate-critique /tmp/hegel-synthesis-codex-$$-r{r}.json
```

**Claude synthesis candidate:**

Read the same rendered synthesis prompt and produce JSON conforming to the same schema. `author=claude`, issue ids C1..C5. Save to `/tmp/hegel-synthesis-claude-$$-r{r}.json` and validate.

### Phase D — Pick / Merge / Apply

Read both synthesis candidates. Decide:

- **Identical or near-identical** (same `summary`, overlapping `next_steps`) → use either as final synthesis, prefer Claude's framing.
- **Different but compatible** (no contradictions, complementary `next_steps`) → merge: union the `next_steps`, longer `summary`, union `concessions`, prefer the candidate with stronger evidence in `issues[]`.
- **Genuine conflict** (contradictory `next_steps` or summaries pointing in opposite directions) → record an `open_disagreements[]` entry, pick the candidate with more verifiable evidence (file:line references in `issues[]` / `next_steps[]`), and surface the conflict in the LOG with both positions.

Save the chosen final synthesis to `/tmp/hegel-synthesis-final-$$-r{r}.json`.

**Apply** the synthesis to the artifact:

| Mode | Apply step |
|---|---|
| **plan** | `Edit` the plan file to reflect each entry in synthesis `next_steps[]`. |
| **implement** | `Edit`/`Write` source files per `next_steps[]`. **If the user chose "Preview every edit" in Step 0, show diff and pause for confirmation. Otherwise auto-apply minor edits, pause on scope changes** (file deletions, new dependencies, public API renames). After applying: `$HEG_BIN smoke-check build`. If FAIL, **revert this round's edits** and surface to user; on second consecutive FAIL, exit loop. |
| **verify** | `Edit`/`Write` test/spec/CI files per `next_steps[]`. After applying: `$HEG_BIN smoke-check test`. Same revert/exit rule. |
| **review** | **No edits.** Append the synthesis to `HEGELIAN-REVIEW.md` (file path: repo root). Each round's synthesis becomes a new section. |

Append a row to the LOG:

```bash
$HEG_BIN log row $LOG {r} antithesis    <verdict> "<K-issue-ids>" "<concessions>"
$HEG_BIN log row $LOG {r} synthesis-codex  <verdict> "<remaining-issue-ids>" "<concessions>"
$HEG_BIN log row $LOG {r} synthesis-claude <verdict> "<remaining-issue-ids>" "<concessions>"
$HEG_BIN log row $LOG {r} synthesis-final  <verdict> "<remaining-issue-ids>" "<merge-note>"
$HEG_BIN status verdict "$$" antithesis    <verdict> <issue-count>
$HEG_BIN status verdict "$$" synthesis    <verdict> <issue-count>
```

For implement/verify modes, also append the smoke-check line via `$HEG_BIN log smoke $LOG "<smoke-check stdout line>"`.

### Phase E — Check stop conditions

In order:

1. **Convergence:** the antithesis (Phase B) returned `verdict=converged` AND its `issues[]` contains zero substantive items (severity ∈ {`critical`, `high`}). The dialectic has reached a position the antithesis cannot meaningfully oppose. Exit loop → Step 3.
2. **Stalled synthesis:** the round's final synthesis is structurally identical to its thesis (no `next_steps[]` actually changed the artifact, OR synthesis `next_steps[]` is empty). The dialectic isn't producing new positions. Exit loop → Step 4.
3. **Smoke-check failure repeat** (implement/verify only): two consecutive smoke-check failures → exit loop → Step 4.
4. **Max rounds:** if `r == N` (default N=3) and not converged → exit loop → Step 4.

Otherwise: synthesis becomes next round's thesis. Increment `r`, go back to Phase A.

## Common process

### Step 0 — Preflight + mode + scope

1. **Run preflight first:** `$HEG_BIN preflight <mode>`. If it fails, stop and surface the failure list. If `bin/converge` is missing, run `bash /Users/mikelady/dev/mike-skills/converge/build.sh`.
2. Determine mode from the argument or infer from context.
3. Identify the artifact (same rules as converge):
   - **plan:** `$HEG_BIN resolve-plan [user-supplied-path]`
   - **implement:** goal statement + directory scope (default: project root)
   - **verify:** packages under verification + verification toolchain
   - **review:** base branch + diff range via `$HEG_BIN detect-base-branch [<pr#>]` and `$HEG_BIN get-diff <base> [<pr#>]`
4. Initialize status: `$HEG_BIN status start "$$" <mode> 3`. Update at every phase boundary with `$HEG_BIN status round "$$" <round> <phase>`.
5. Confirm scope and stop conditions via **one** AskUserQuestion call. Defaults shown — accept unless changed:
   - **Max rounds:** 3
   - **Convergence threshold:** "antithesis returns verdict=converged with zero substantive issues"
   - **Mode-specific:**
     - `implement` only: "Auto-apply minor edits, pause on scope changes" (default) or "Preview every edit"
     - `review` only: "No working-tree edits" (always)
6. Print: `Dialectic on <artifact>. Mode: <mode>. Up to 3 rounds (thesis → antithesis → synthesis each).`

### Step 1 — Establish the LOG location

| Mode | Log file |
|---|---|
| plan | Append `## HEGELIAN LOG` section to the plan file |
| implement | Create/append `HEGELIAN-LOG.md` at repo root |
| verify | Create/append `HEGELIAN-LOG.md` at repo root |
| review | Output goes to `HEGELIAN-REVIEW.md` at repo root |

```bash
$HEG_BIN log init <log-path>
```

This writes the standard header and a new dated `### Run YYYY-MM-DD HH:MM` subsection.

### Step 2 — Round loop

Run rounds 1..N as described above (Phases A–E). At any phase boundary the user can interrupt; the artifact is always in a consistent post-apply (and post-smoke-check, where applicable) state at round boundaries.

### Step 3 — Convergence path

Print:

```
✅ Dialectic converged on <artifact>. Mode: <mode>. R rounds (thesis → antithesis → synthesis).

Final synthesis: <one-paragraph summary>
Antithesis cap: <last-round antithesis verdict + issue count>
Synthesis movement: <"thesis → synthesis delta lines" or "edits applied">
Concessions made: claude=N, codex=M
Smoke checks: <pass count>/<total run> (implement/verify only)
```

Append a `### Synthesized Result` subsection to the LOG (one-paragraph synthesized position + the consequential changes from original).

For `review` mode, write the final block to `HEGELIAN-REVIEW.md`:

```markdown
# Hegelian Review of <branch> against <base>

**Verdict:** APPROVED — synthesis converged after R rounds; antithesis could not construct further opposition.

## Synthesized position
<final synthesis summary paragraph>

## Findings retained
<issues that survived synthesis with severity ∈ {critical, high}>

## Concessions
- Claude conceded: <list>
- Codex conceded: <list>
```

Stop.

### Step 4 — Stalled / max-rounds / smoke-fail path

Print the unresolved tension and surface to user via AskUserQuestion. Use this preview structure per unresolved item (typically one per round that didn't converge):

```
UNRESOLVED — <one-line subject>

Mode: <mode>
Reason: <stalled | max-rounds | smoke-fail>

Last thesis: <one-line summary>
Last antithesis: <one-line summary>
Last synthesis: <one-line summary>

Codex's strongest opposition: <one sentence from antithesis summary>
Synthesis attempt's residual gap: <one sentence>

Recommendation: <if Claude's synthesis was stronger than Codex's, name it; else "genuine taste call — pick the direction that fits your priorities">
```

Options:
- A) Accept Claude's last synthesis candidate
- B) Accept Codex's last synthesis candidate
- C) Accept the merged final synthesis
- D) Hybrid (user describes how to reconcile)
- E) Defer — record as unresolved in LOG, leave artifact in post-last-synthesis state

Apply the user's choice (skip apply for `review` mode — record verdict in `HEGELIAN-REVIEW.md`). Append the user's decision to the LOG with author=`user`.

For `review` mode, write the final block to `HEGELIAN-REVIEW.md`:

```markdown
# Hegelian Review of <branch> against <base>

**Verdict:** CHANGES_REQUESTED (or BLOCKED if smoke-fail) — dialectic did not converge after R rounds.

## Last synthesis attempt
<final synthesis summary paragraph>

## Unresolved tensions
<each open_disagreement with both positions>

## User decisions
<user's pick per unresolved item>
```

Print:

```
🤝 Reached negotiated state on <artifact>. Mode: <mode>. R rounds.
Resolution: partial — <count> unresolved items arbitrated by user.
Log: <log path>
```

### Step 5 — Cleanup + finalize status

```bash
$HEG_BIN status end "$$" {converged|stalled|max-rounds|smoke-fail|error}
$HEG_BIN cleanup
rm -f /tmp/hegel-*-$$-r*.{md,json,txt}    # hegel-specific tmp files
```

The LOG / REVIEW files and the `status end`-finalized snapshot are the deliverable.

## Mode-specific guidance

### plan mode

- Cheapest mode (no smoke checks). Soft convergence usually triggers in 1–2 rounds for well-formed plans.
- Round 1's thesis is the existing plan file. Antithesis tries to break confidence in the plan. Synthesis produces a revised plan.
- The plan file IS the working state; every applied synthesis edits it in place.
- A "stalled synthesis" outcome usually means the plan is either already well-formed (good) or the antithesis is over-fitting to one weakness (less common; user judgment).

### implement mode

- Treats the active plan (if any) as the contract. Convergence ≠ "code is perfect" — it's "the latest synthesis incorporates everything the antithesis can throw at it within available evidence."
- Auto-apply only when the synthesis's `next_steps[]` are single-function changes with no API surface impact. Anything touching public types, public functions, or dependency files (`go.mod`, `Cargo.toml`, `package.json`) requires preview confirmation.
- Run smoke check (`build`) every round. Two consecutive failures exit to Step 4.
- Pair with `/hegelian-dialectic verify` afterward — implement convergence does not guarantee adequate test coverage.

### verify mode

- Antithesis focuses on what's *not* tested — coverage gaps, missing property tests, weak invariants.
- Synthesis proposes new tests and stronger invariants.
- Run `smoke-check test` every round. Two consecutive failures exit.
- Both reasoners are explicitly told that "100% line coverage" is a weak signal alone.

### review mode

- Read-only; the deliverable is `HEGELIAN-REVIEW.md`.
- Round 1's thesis is **Claude's own review of the diff** (in critique-JSON form). Antithesis attacks Claude's review (challenges findings, proposes counter-findings). Synthesis produces an integrated review.
- Output verdict: `APPROVED` (converged with no critical findings), `APPROVED_WITH_NITS` (only minor remain), `CHANGES_REQUESTED` (≥1 critical/high finding survives), `BLOCKED` (stalled / unresolved tension).

## Failure modes & edge cases

- **Codex auth error:** stop, tell user to run `codex login`. Artifact left at last consistent state; LOG records the partial run.
- **Plan file or diff is huge (>50KB):** warn the user; codex exec input has practical limits. Offer to dialect a subset.
- **Round 1 antithesis converges immediately:** still write the LOG with the single round; this is a successful no-op review (the thesis already withstands opposition).
- **Both synthesis candidates are empty:** that's a stall — the antithesis didn't surface anything actionable. Exit to Step 4.
- **Malformed JSON from codex:** retry once with stricter "JSON only" suffix. If still malformed, treat that codex output as `verdict=converged` with empty `issues[]` and let Claude's candidate drive — note in LOG.
- **User interrupts mid-round:** the artifact is at a consistent post-apply state at round boundaries, NOT mid-round. Mid-round interruption may leave the artifact in an inconsistent state — recommend user restart the run.
- **Implement mode breaks the build twice:** stop, surface the last passing state and the failing diff. Exit to Step 4.
- **Review mode and the diff is empty:** no work to do; tell the user.

## Watching a running session

The status snapshot is updated at every phase boundary. To peek mid-run:

```bash
$HEG_BIN status show <session-id>          # one-shot
watch -n 2 $HEG_BIN status show <session-id>   # polling

# Or follow the file directly:
tail -F "$($HEG_BIN status path <session-id>)"
```

Session id is `$$` of the prompt that started the run.

## What this skill is NOT

- Not a way to launder a bad artifact into a "verified" one. Antithesis-converges means codex couldn't construct opposition within its training and the available evidence — not that the artifact is correct.
- Not a substitute for `/converge` when you don't want the synthesis-as-deliverable structure. Use `/converge` for parallel-critique style; use this skill when you want an explicit synthesized position as output.
- Not for one-shot reviews — if you only want a single critique, use `/codex review`.
- Not an autonomous coder — implement mode pauses on scope-shifting edits and bails on broken builds.

## Common Mistakes

- **Skipping the pick step.** "Both synthesis candidates look good" is not a valid pick. Read both, identify whether they agree/are-compatible/conflict, then merge or pick deliberately.
- **Letting the antithesis become a critique without an opposing position.** The antithesis prompt asks for the *strongest opposing case*, not a list of nits. If codex returns nits, re-prompt with stricter "construct an opposing position, not a critique" suffix.
- **Skipping smoke checks in implement/verify.** The whole point of those modes is that the synthesis must keep the artifact buildable/testable. Don't move to round N+1 without running the smoke check after applying round N's synthesis.
- **Reusing converge's `REVIEW.md` filename in review mode.** Always write to `HEGELIAN-REVIEW.md` to avoid clobbering output from a prior `/converge review` run on the same branch.
- **Forgetting that `additionalProperties: false`** on the schema means you can't add `synthesis_position` or other custom fields. Use `summary` + `next_steps[]` to carry the synthesized position.
- **Resuming the codex thread across rounds.** The thread is per-round (Phase B fresh in round 1, Phases B/C resumed within the same round). Round N+1's antithesis call should start a fresh thread, because the synthesis applied between rounds substantively changed the artifact and the prior thread's context would be misleading. Capture a new `THREAD_ID` at the start of each round.
