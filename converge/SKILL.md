---
name: converge
description: Use when the user wants Claude, Codex, agy, and the Cursor models Composer 2.5 and Grok Build to iterate together until they converge on a mutually-agreed result — or hit a deadlock the user must arbitrate — and for fresh-eyes adversarial review of drafted artifacts. Five modes — plan, implement, verify, review, and audit (the folded-in adversarial review). Triggers — "converge", "have claude and codex work it out", "iterate until you agree", "three-AI consensus", "multi-AI consensus", "deadlock me a decision", "adversarial review this", "audit my drafts", "fresh eyes on this", "review for fabrications".
user_invocable: true
---

# converge

Multi-AI iterative refinement across the full development lifecycle. Claude (this agent), Codex (via the `codex` CLI), agy (via the `agy` CLI), and two Cursor models — Composer 2.5 and Grok Build (both via the Cursor `agent` CLI) — take turns critiquing and revising an artifact until either:

1. **Convergence** — all reviewers agree the artifact is sound (within a bounded change-rate threshold), OR
2. **Deadlock** — they disagree on a decision none can resolve from first principles, at which point the user is presented with each side's *best argument* and makes the call.

The four **negotiation** modes (plan/implement/verify/review) share one core convergence loop and differ only in the artifact, the critique prompt, the apply-fixes semantics, and the deliverable. A fifth mode, **audit**, is the folded-in *adversarial review*: a single-shot, fresh-eyes, N-way fan-out (claude + codex + agy + composer-2.5 + grok-build) with FAIL-OR merge over arbitrary artifacts — no negotiation; it's the primitive other skills call. (`audit` absorbed the former standalone `adversarial-review` skill.)

## Modes

| Mode | Argument | Artifact | Apply-fixes | Deliverable |
|---|---|---|---|---|
| **plan** | `/converge plan` | A plan markdown file (default: most recent in `~/.claude/plans/`) | `Edit` on the plan file | Updated plan + `## CONVERGE LOG` footer |
| **implement** | `/converge implement` | The working tree against a stated goal (typically tied to a plan file) | `Edit`/`Write` on source files, with preview-then-apply on each diff | Working tree at converged state + `CONVERGE-LOG.md` at repo root |
| **verify** | `/converge verify` | Test suites + formal-verification specs + CI config | `Edit`/`Write` on test/spec/CI files | Updated tests/specs/CI + `CONVERGE-LOG.md` |
| **review** | `/converge review` | A diff against a base branch (local branch or PR #) | **No auto-apply** — produces findings only | `REVIEW.md` with cited issues + verdict |
| **audit** | `/converge audit` | Arbitrary drafted artifacts (posts, plans, docs, tests, or a diff) composed into one prompt | **No auto-apply** — single-shot, no rounds | Canonical merged JSON (`{summary, verdicts[], reviewers}`) returned to the caller |

`audit` is the **adversarial-review fold**: a fresh-eyes, N-way fan-out with FAIL-OR merge — not the iterative negotiation the other four run. See the `audit` mode section below.

If the user types just `/converge` with no mode, infer from context (active plan file → `plan`; uncommitted changes → `implement` or `review`; otherwise ask) and confirm via AskUserQuestion before proceeding.

## Requirements

- Reviewer CLIs on PATH: `codex` (`npm install -g @openai/codex`), `agy`, and the Cursor `agent` CLI (carries the `composer-2.5` and `grok-build` models); `claude` is this agent. Negotiation and `audit` both run claude + codex + agy + composer-2.5 + grok-build by default. composer-2.5/grok-build need a paid Cursor plan — without one they quota-fail and are reported under `skipped` (audit) and noted as skips in negotiation; a reviewer whose CLI is absent is likewise `skipped`. Drop reviewers from negotiation via `CONVERGE_REVIEWERS` (or narrow `--reviewers` for audit).
- The transport binary at `bin/converge` (built from the Go source in `go/`). If missing, run `bash build.sh` from the skill root — needs Go 1.25+, no external deps.
- For modes `implement`, `verify`, `review`: a git repository at the working directory.
- For mode `review`: either uncommitted changes, an active branch with commits ahead of base, or an explicit PR # passed as `/converge review <PR>`.

## Transport binary (always call this, do not re-derive)

All transport work — codex invocation, diff retrieval, log formatting, schema validation, prompt rendering, smoke checks, status snapshots — lives in a single Go binary at **`bin/converge`**. The schemas and prompt templates ship embedded in the binary; sources live in `go/internal/embedded/{schemas,prompts}/`.

| Subcommand | Purpose |
|---|---|
| `bin/converge preflight <mode>` | Verify codex on PATH + authenticated, git repo (where required), gh CLI present (review). Run this in Step 0 before doing anything else. |
| `bin/converge resolve-plan [path]` | Resolve plan file (explicit > `$CONVERGE_ACTIVE_PLAN` > repo-slug match in `~/.claude/plans/` > most-recent). |
| `bin/converge detect-base-branch [pr#]` | `gh pr view` → `gh repo view` → `origin/HEAD` → `origin/main`/`origin/master`. |
| `bin/converge get-diff <base> [pr#]` | `git diff base...HEAD` or `gh pr diff`, truncated to 50KB (`$CONVERGE_DIFF_MAX_BYTES`). |
| `bin/converge render-prompt <mode> KEY=… ...` | Render the embedded `<mode>.tmpl` template (plan/implement/verify/review) with `{{PLACEHOLDER}}` substitution. `KEY=value` literal or `KEY=@/path` to read a file. `{{IF_RESUME}}…{{ENDIF_RESUME}}` blocks toggle on `RESUME=1`. |
| `bin/converge codex-critique [--resume <thread-id>] [--model <m>] <prompt-file> [effort]` | Run `codex exec`. Streams `[codex Ns] reasoning/tool/message` events to stderr so the caller sees codex is alive. Stdout = final assistant message only. Round 1: starts a new thread, captures the thread id at `$CONVERGE_THREAD_OUT` (default `/tmp/converge-thread-<pid>.txt`). Rounds 2..N: pass `--resume <thread-id>` so codex doesn't re-read the artifact — round prompts include only the delta. Model: `--model` / `$CONVERGE_CODEX_MODEL`, else codex's `~/.codex/config.toml` default (currently `gpt-5.5`). Exits 3 (auth) / 4 (timeout) / 5 (no message). |
| `bin/converge claude-critique [--resume <session-id>] [--model <m>] <prompt-file> [effort]` | Same shape as `codex-critique` but routes through the `claude` CLI (`claude -p ... --output-format stream-json`). Captures the session UUID for resume. `--model` defaults to `opus` (override with `$CONVERGE_CLAUDE_MODEL`). Same exit codes. |
| `bin/converge llm-critique --provider {codex\|claude\|agent\|agy} [--resume <id>] [--model <m>] <prompt-file> [effort]` | Generic form — pick provider explicitly (`agy` replaced the deprecated `gemini`). The two `*-critique` subcommands are aliases. The Cursor models run through `--provider agent --model composer-2.5` and `--provider agent --model grok-build-0.1`. |
| `bin/converge audit [--reviewers claude,codex,agy,composer-2.5,grok-build] [--prompt-file <p>] [--timeout <s>] [--quiet]` | **Adversarial review (fresh-eyes fan-out).** Fan the SAME composed prompt to all reviewers in parallel, parse each reviewer's JSON verdict, FAIL-OR merge with `[r1+r2+...]` issue attribution + graceful `skipped` degradation, emit canonical `{summary, verdicts[], reviewers, skipped}` JSON. Prompt from `--prompt-file` or stdin. Registered reviewers: claude, codex, agent, composer-2.5, grok-build, agy (composer-2.5/grok-build both dispatch via the Cursor `agent` CLI, pinned to distinct models). Used by `audit` mode + `review` round 1. |
| `bin/converge validate-critique <json>` | Validate against the embedded JSON Schema. Set `CONVERGE_REQUIRE_EVIDENCE=1` for implement/verify/review (forces `file`+`line_start`+`line_end`). |
| `bin/converge smoke-check build\|test` | Project-type detection (`go.mod`, `Cargo.toml`, `package.json`, `pyproject.toml`) and run. Override with `$CONVERGE_SMOKE_BUILD` / `$CONVERGE_SMOKE_TEST`. |
| `bin/converge log {init\|row\|smoke\|note} <file> ...` | LOG / REVIEW.md writer — header, dated `### Run YYYY-MM-DD HH:MM` subsection, table rows, smoke-check lines, free-form notes. |
| `bin/converge status {start\|round\|thread\|verdict\|end\|path\|show} <session-id> ...` | Per-round status snapshot at `${CONVERGE_STATUS_DIR:-/tmp}/converge-status-<sid>.json` so the user (or another agent) can `tail -F` the run. Use `$$` (your prompt's PID) as the session id. |
| `bin/converge cleanup` | Remove `/tmp/converge-*` per-round payloads. Logs/REVIEW are deliverables; left alone. |

When you invoke `codex-critique` (or `claude-critique` / `llm-critique`), **leave its stderr connected to your terminal** so the user sees the heartbeat. Set `CONVERGE_QUIET=1` only if explicitly asked.

**Reviewers (5-way default).** Negotiation runs five independent reviewers — **claude** (this agent, in-context), **codex** (`codex-critique`), **agy** (`llm-critique --provider agy`), **composer-2.5** (`llm-critique --provider agent --model composer-2.5`), and **grok-build** (`llm-critique --provider agent --model grok-build-0.1`). The per-mode templates are **author-neutral**: render them with `REVIEWER_NAME`, `AUTHOR`, and `ID_PREFIX` (C=claude, K=codex, A=agy, M=composer-2.5, G=grok-build) so the *same* template serves every non-claude reviewer. agy, composer-2.5, and grok-build are treated as **one-shot** (no thread resume), so on rounds 2..N resend the round delta rather than `--resume` (the Cursor `agent` CLI *does* support `--resume`; one-shot just keeps the loop uniform — wiring resume for it is a future optimization). The agent honors `CONVERGE_REVIEWERS` (default `claude,codex,agy,composer-2.5,grok-build`) to decide which reviewer passes to run — set e.g. `CONVERGE_REVIEWERS=claude,codex` for a faster 2-way run. composer-2.5/grok-build need a paid Cursor plan; if it quota-fails, note the skip and judge convergence over the reviewers that responded.

**Model tier — highest by default.** Each reviewer runs at its top tier:
- **claude → `opus`** (the default; override with `$CONVERGE_CLAUDE_MODEL` or `--model`).
- **codex → `--model` / `$CONVERGE_CODEX_MODEL`, else its `~/.codex/config.toml` `model`** (currently `gpt-5.5`). Pin it explicitly for reproducibility rather than relying on the user's config.
- **agy → `~/.gemini/settings.json` → `model.name`** (agy is a Gemini CLI; it has **no** `--model` flag or env var, so converge can't pin it per-run — set it once in that file). Currently **`gemini-3.1-pro`** (top tier). To sanity-check a model name is honored: a valid one resolves in seconds, a bogus one hangs (agy reads `model.name` and has no silent fallback).
- **composer-2.5 → `--provider agent --model composer-2.5`** (Cursor's Composer 2.5).
- **grok-build → `--provider agent --model grok-build-0.1`** (Cursor's Grok Build 0.1 1M). Both Cursor models are pinned per-run via `--model`; confirm available names with `agent --list-models`.

For maximum rigor pair the top model with **`high` reasoning effort** via the trailing `[effort]` arg on `codex-critique`/`claude-critique` (e.g. `bin/converge codex-critique <prompt> high`).

## Common process

### Step 0 — Preflight + mode + scope

1. **Run preflight first:** `bin/converge preflight <mode>`. If it fails, stop and surface the failure list — don't try to start a round loop without a working toolchain. If `bin/converge` is missing, run `bash build.sh` to build it.
2. Determine mode from the argument or infer it.
3. Identify the artifact:
   - **plan:** plan file path. Run `bin/converge resolve-plan [user-supplied-path]`. Set `CONVERGE_ACTIVE_PLAN` if the conversation already names a plan in flight.
   - **implement:** the goal statement (from active plan file or user prompt) + the directory scope (default: project root, or a subdirectory the user names).
   - **verify:** the package(s) under verification + the verification toolchain (tests only, formal verifier, both). Detect via `bin/converge smoke-check test`; for formal verifiers (Gobra/Verus), the prompt picks them.
   - **review:** the base branch + diff range. Run `bin/converge detect-base-branch [<pr#>]` to get the base. Use `bin/converge get-diff <base> [<pr#>]` to fetch a 50KB-truncated diff for codex prompts.
4. Initialize the status snapshot: `bin/converge status start "$$" <mode> <max-rounds>`. Update it at every phase boundary with `bin/converge status round "$$" <round> <phase>`.
5. Confirm scope and stop conditions with the user via **one** AskUserQuestion call. Defaults shown — accept them unless changed:
   - **Max rounds:** 5
   - **Convergence threshold:** "all reviewers return ≤2 substantive issues (union) AND ≥1 explicit agreement signal"
   - **Deadlock surface:** "any single decision where reviewers have re-stated opposing positions across 2 consecutive rounds"
   - **Mode-specific:**
     - `implement` only: "Auto-apply minor edits, pause on major scope changes" (default) or "Preview every edit"
     - `review` only: "Findings only, no auto-fix" (default)
6. Print: `Converging on <artifact>. Mode: <mode>. Up to N rounds. I'll surface deadlocks for you to decide.`

### Step 1 — Establish the LOG location

| Mode | Log file |
|---|---|
| plan | Append `## CONVERGE LOG` section to the plan file |
| implement | Create/append `CONVERGE-LOG.md` at the repo root |
| verify | Create/append `CONVERGE-LOG.md` at the repo root |
| review | Output goes into `REVIEW.md` at the repo root |

Run `bin/converge log init <log-path>` — it ensures the standard header exists and appends a new dated `### Run YYYY-MM-DD HH:MM` subsection so prior runs aren't overwritten. Don't hand-write the header.

### Step 2 — Round loop

For round `r` from 1 to N:

#### 2a. Claude critique pass

Produce a structured critique conforming to the JSON Schema at `go/internal/embedded/schemas/critique.schema.json` (also embedded in the binary). Save it to `/tmp/converge-claude-r{r}.json`, then validate with:

```bash
CONVERGE_REQUIRE_EVIDENCE=1 bin/converge validate-critique /tmp/converge-claude-r{r}.json
# (omit CONVERGE_REQUIRE_EVIDENCE for plan mode)
```

Show the JSON in conversation as a fenced ```json block.

Required fields (see schema for the full contract): `round`, `author`="claude", `mode`, `verdict` (`needs_revision`|`converged`), `summary` (terse ship/no-ship), `issues[]` (max 5; each with `id`=Cn, `severity`=critical|high|medium|low, `title`, `body`, `confidence`∈[0,1], `recommendation`; plus `file`+`line_start`+`line_end` for implement/verify/review). Optional: `concessions[]`, `open_disagreements[]`, `next_steps[]`.

Mode-specific critique focus:

| Mode | What Claude critiques |
|---|---|
| **plan** | Logical gaps, unstated assumptions, missing edge cases, overcomplexity, feasibility risk, missing dependencies, missing test/rollback/migration plan, observability gaps |
| **implement** | Plan/code drift, untested branches, error handling, race conditions, idempotency, rollback safety, version skew, observability gaps, missing fixtures |
| **verify** | Coverage gaps (real coverage, not a manifest grep), missing property tests, missing negative tests, weak invariants, unverified preconditions, race conditions, CI gate adequacy |
| **review** | Auth / tenancy / trust boundaries; data loss / corruption / irreversibility; rollback safety / retries / idempotency; race conditions / ordering; empty-state / null / timeout; version skew / migration hazards; observability gaps; PR scope creep |

Rules for every Claude pass:
- Cap `issues` at 5. If more emerge, keep top by `severity × confidence` and add the rest to `next_steps[]`.
- An issue is "substantive" if `severity` ∈ {`critical`, `high`}. `medium`/`low` do not block convergence.
- Each `recommendation` must be concrete and testable, not "consider X."
- For implement/verify/review, every issue MUST include `file` + `line_start` + `line_end`. No location ⇒ drop the issue.
- `confidence` is honest probability the finding holds — lower it for inferences.
- If zero substantive issues remain AND no open disagreements, set `verdict` to `converged`.

#### 2b. Apply Claude's proposed fixes (skip in `review` mode)

For each issue in 2a where Claude proposes a fix:
- **plan mode:** edit the plan file via `Edit`.
- **implement mode:** edit source files via `Edit`/`Write`. **If the user chose "Preview every edit" in Step 0, show the diff and pause for confirmation. Otherwise auto-apply minor edits, but pause on major scope changes** (file deletions, new dependencies, public API renames).
- **verify mode:** edit test/spec/CI files via `Edit`/`Write`. Run the verifier/test suite after each batch of edits and capture pass/fail in the LOG.
- **review mode:** **do not apply fixes.** Append the issue to the in-progress `REVIEW.md` instead.

For implement/verify modes, after applying fixes, run a smoke check via `bin/converge smoke-check build` (implement) or `bin/converge smoke-check test` (verify). It detects project type and runs the right command; override with `$CONVERGE_SMOKE_BUILD` / `$CONVERGE_SMOKE_TEST` if the project needs something custom. Capture its stdout line and append via `bin/converge log smoke <log> "<line>"`. If it prints `FAIL`, **revert the round's edits** and surface the failure to the user before proceeding.

#### 2c. Codex, agy, composer-2.5, and grok-build critique passes (each independent)

Run this pass once per non-claude reviewer in `CONVERGE_REVIEWERS` (default: `codex`, then `agy`, `composer-2.5`, `grok-build`). Render the prompt from the embedded mode template — don't compose it inline. The templates are **author-neutral** tagged XML contracts; fill `REVIEWER_NAME`, `AUTHOR`, and `ID_PREFIX` so the same template serves each reviewer. `PRIOR_CRITIQUES` is the other reviewers' critiques available so far this round (at minimum claude's; include each earlier reviewer's critique when rendering a later one so it can concede to them). Sources: `go/internal/embedded/prompts/<mode>.tmpl`.

```bash
ARTIFACT_FILE=/tmp/converge-artifact-r{r}.txt    # plan: full plan; implement/review: get-diff output; verify: tests+specs
PRIOR_FILE=/tmp/converge-prior-r{r}.txt          # other reviewers' critiques this round (claude's; +earlier reviewers')
LOG_FILE=/tmp/converge-priorlog-r{r}.txt          # tail of the LOG table

# codex → ID_PREFIX=K ; agy → A ; composer-2.5 → M ; grok-build → G (REVIEWER_NAME and AUTHOR = the reviewer name)
bin/converge render-prompt <mode> \
  ROUND={r} MAX_ROUNDS={N} RESUME={0 or 1} \
  REVIEWER_NAME=<reviewer> AUTHOR=<reviewer> ID_PREFIX=<K|A|M|G> \
  ARTIFACT=@$ARTIFACT_FILE \
  PRIOR_LOG=@$LOG_FILE \
  PRIOR_CRITIQUES=@$PRIOR_FILE \
  > /tmp/converge-prompt-<reviewer>-r{r}.txt
```

**codex** (thread-resumed — round 1 starts a thread, rounds 2..N resume it so codex re-reads only the delta; render with `RESUME=1` on rounds ≥2):

```bash
# round 1
bin/converge codex-critique /tmp/converge-prompt-codex-r{r}.txt > /tmp/converge-codex-r{r}.json
THREAD_ID=$(cat "${CONVERGE_THREAD_OUT:-/tmp/converge-thread-$$.txt}")
bin/converge status thread "$$" "$THREAD_ID"
# rounds 2..N
bin/converge codex-critique --resume "$THREAD_ID" /tmp/converge-prompt-codex-r{r}.txt > /tmp/converge-codex-r{r}.json
```

**agy, composer-2.5, grok-build** (one-shot — no thread resume; render each with `RESUME=0` and carry the round delta in `PRIOR_CRITIQUES` each round). composer-2.5 and grok-build both route through the Cursor `agent` CLI, pinned to distinct models via `--model`:

```bash
bin/converge llm-critique --provider agy                          /tmp/converge-prompt-agy-r{r}.txt      > /tmp/converge-agy-r{r}.json
bin/converge llm-critique --provider agent --model composer-2.5   /tmp/converge-prompt-composer-2.5-r{r}.txt > /tmp/converge-composer-2.5-r{r}.json
bin/converge llm-critique --provider agent --model grok-build-0.1 /tmp/converge-prompt-grok-build-r{r}.txt   > /tmp/converge-grok-build-r{r}.json
```

Leave each call's stderr connected so the user sees the heartbeat. Exit codes (both): `3` (auth) → stop, tell the user to log in to that CLI; `4` (timeout) → treat that reviewer as skipped-by-timeout for the round and proceed with the rest; `5` (no message) → retry once with a stricter "Respond with JSON only" prompt, else treat that reviewer's verdict as `converged` for the round and note in LOG.

Validate each response (omit `CONVERGE_REQUIRE_EVIDENCE` for plan mode):

```bash
CONVERGE_REQUIRE_EVIDENCE=1 bin/converge validate-critique /tmp/converge-codex-r{r}.json
CONVERGE_REQUIRE_EVIDENCE=1 bin/converge validate-critique /tmp/converge-agy-r{r}.json
CONVERGE_REQUIRE_EVIDENCE=1 bin/converge validate-critique /tmp/converge-composer-2.5-r{r}.json
CONVERGE_REQUIRE_EVIDENCE=1 bin/converge validate-critique /tmp/converge-grok-build-r{r}.json
```

#### 2d. Apply each non-claude reviewer's proposed fixes

Same edit rules as 2b, applied for each reviewer's (codex, agy, composer-2.5, grok-build) accepted fixes. Skip in `review` mode (findings only).

#### 2e. Update CONVERGE LOG + status

Append one row per reviewer pass:

```bash
bin/converge log row <log> {r} claude       needs_revision "C1, C2" "(none)"
bin/converge log row <log> {r} codex        needs_revision "K1, K2" "C1"
bin/converge log row <log> {r} agy          converged      "(none)" "C2, K1"
bin/converge log row <log> {r} composer-2.5 needs_revision "M1"     "C1"
bin/converge log row <log> {r} grok-build   converged      "(none)" "K1"
bin/converge status verdict "$$" claude       needs_revision 2
bin/converge status verdict "$$" codex        needs_revision 2
bin/converge status verdict "$$" agy          converged      0
bin/converge status verdict "$$" composer-2.5 needs_revision 1
bin/converge status verdict "$$" grok-build   converged      0
```

For `implement`/`verify`, also append the smoke-check line via `bin/converge log smoke <log> "<smoke-check stdout line>"` after each round that ran one.

#### 2f. Check stop conditions

- **Convergence:** **all active reviewers** return `verdict == "converged"` in the same round → exit loop, go to Step 3.
- **Soft convergence:** all verdicts are `needs_revision` but the union of substantive `issues` across all passes ≤ 2 AND no `open_disagreements` → ask the user via AskUserQuestion whether to call it converged or run one more round. Default: converge.
- **Deadlock:** the same `open_disagreements` entry (matched by its `topic` + `positions` set, or by repeated issue ids referenced as "still disputing K3") appears in 2 consecutive rounds → exit loop, go to Step 4.
- **Max rounds:** if `r == N` and not converged, exit loop, go to Step 4 with the unresolved disagreements.
- **Smoke check failed twice in a row** (implement/verify only): exit loop, go to Step 4 — the artifact is not stable enough for further iteration without user input.
- **A reviewer is unavailable** (auth/quota/timeout): proceed with the remaining reviewers for that round, note the skip in the LOG, and judge convergence over the reviewers that responded.

### Step 3 — Convergence path

Print:

```
✅ All reviewers (claude + codex + agy + composer-2.5 + grok-build) converged on <artifact>. Mode: <mode>. R rounds.

Final state: <one-paragraph summary of what changed from the original>
Issues resolved: <count>
Concessions made: claude=N, codex=M, agy=P, composer-2.5=Q, grok-build=S
Smoke checks: <pass count>/<total run> (implement/verify only)
```

Then write a final `### Converged Result Summary` subsection to the LOG capturing:
- **plan:** original plan one-liner → final plan one-liner; 3-5 most consequential changes
- **implement:** files changed, LOC delta, plan-coverage assessment ("plan F1-F4 implemented; F5 deferred")
- **verify:** coverage delta, new property tests added, verifier-obligation count delta
- **review:** N/A (review mode goes through Step 4 always — see below)

For `review` mode, "convergence" means all reviewers signed off with verdict `converged`. Write `REVIEW.md` with:

```markdown
# Review of <branch> against <base>

**Verdict:** APPROVED — all reviewers (claude + codex + agy + composer-2.5 + grok-build) signed off after R rounds.

## Findings (resolved during review)
<all issues raised across rounds, marked resolved>

## Reviewer notes
<any minor non-blocking observations>
```

Stop.

### Step 4 — Deadlock / unresolved path

For each unresolved `open_disagreement`, present the user a structured arbitration block. Use AskUserQuestion **one decision at a time** (per gstack AskUserQuestion conventions), with this exact preview structure:

```
DEADLOCK #N — <topic>

Mode: <mode>
Affected: <file path / plan section / test name>

Positions (one block per party in the open_disagreement's positions[]):
  <author>: <position>
    Best argument: <that reviewer's strongest single sentence>
    Cost if another party is right: <one sentence>
  ... repeat for each of claude / codex / agy / composer-2.5 / grok-build present in positions[] ...

Recommendation: <if one position has a 70/30 lean, name it; else "genuine taste call — pick the one that fits your priorities">
```

To get each party's "best argument," do a final adversarial pass per party:
1. claude (you): "You are committed to your position on DEADLOCK #N; the others disagree. Write your single strongest sentence and the cost of being wrong. Do not hedge."
2. codex: send the analogous prompt via `bin/converge codex-critique --resume "$THREAD_ID"`.
3. agy: send the analogous prompt via `bin/converge llm-critique --provider agy`.
4. composer-2.5: `bin/converge llm-critique --provider agent --model composer-2.5`.
5. grok-build: `bin/converge llm-critique --provider agent --model grok-build-0.1`.

(Only run the passes for parties actually present in this deadlock's `positions[]`.)

Options:
- A) Take a specific party's side (name claude / codex / agy / composer-2.5 / grok-build)
- B) Hybrid (user describes how to reconcile)
- C) Defer (mark unresolved in LOG, move on)

Apply the user's choice to the artifact (skip apply for `review` mode — record the decision in `REVIEW.md`). Append the user's decision + rationale to the LOG with author=`user`.

After all deadlocks are arbitrated:

```
🤝 Reached negotiated agreement on <artifact>. Mode: <mode>. R rounds.
Convergence: partial — <D> deadlocks resolved by user, <C> auto-converged items.
Log: <log path>
```

For `review` mode, the final `REVIEW.md` records each deadlock + the user's verdict and includes a `## User decisions` section.

### Step 5 — Cleanup + finalize status

```bash
bin/converge status end "$$" {converged|deadlock|max-rounds|smoke-fail|error}
bin/converge cleanup
```

`cleanup` removes per-round JSON, prompt, and thread-id files under `/tmp/converge-*`. The LOG / REVIEW files and the `status end`-finalized snapshot are the deliverable and are intentionally not touched.

## Audit mode (folded-in adversarial review)

`audit` is the **single-shot, fresh-eyes, N-way fan-out** that replaced the standalone `adversarial-review` skill. It does NOT run the Steps 0–5 negotiation loop — no rounds, no thread resume, no concede/converge. Every selected reviewer sees the SAME prompt independently and their verdicts merge with FAIL-OR. Use it to gate drafted artifacts (social posts, plans, docs, generated tests, a diff) before a human sees them, and as a callable primitive from other skills.

**Fresh eyes are the point.** Pass the reviewers ONLY the source material + rules + drafts. Do NOT include any compose-phase context (intent, history, why the composer chose this) — that's what biases a reviewer into rationalizing problems away.

### Inputs (the caller composes these into one prompt)

| Field | Required | Description |
|---|---|---|
| `source_label` | yes | Label for the source material ("SOURCE ARTICLE", "ORIGINAL FILE", "SPEC"). |
| `source_content` | yes | The full source the drafts must respect. |
| `skill_name` | yes | Calling skill (e.g. `tease-newsletter`, `maximize-verification`). |
| `artifact_name` | yes | Singular noun for each drafted item ("teaser", "patch", "post"). |
| `rules_list` | yes (≥1) | The rules each draft MUST satisfy. |
| `drafts` | yes (≥1) | The drafted items, each with a caller-assigned `id`. |
| `issue_guidance` | optional | Hint on what citations to include. |

Refuse to run (return the `parse_error` shape) if any required field is missing/empty or any draft lacks `content`.

### Flow

1. Assemble ONE prompt from the inputs (the binary owns transport + merge; composing is the caller's job):

```
You are an adversarial reviewer for /<<SKILL_NAME>> <<ARTIFACT_NAME>>s. Find problems before the user has to.

<<SOURCE_LABEL>>:
<<SOURCE_CONTENT>>

RULES (must be enforced):
<<RULES_LIST>>

DRAFTED <<ARTIFACT_NAME>>s:
<<DRAFTS as a numbered list, each with its draft_id and content>>

For each draft return VERDICT ("PASS"/"FAIL") and ISSUES (cite exact substrings).
Return ONLY this JSON, no prose:
{"summary":"all_pass"|"some_fail","verdicts":[{"draft_id":"<id>","verdict":"PASS"|"FAIL","issues":["..."]}]}
```

2. Run the fan-out (default reviewers claude + codex + agy + composer-2.5 + grok-build):

```bash
printf '%s' "$ASSEMBLED_PROMPT" | bin/converge audit
# or: bin/converge audit --prompt-file /tmp/audit-prompt.txt --reviewers claude,codex,agy,composer-2.5,grok-build --timeout 300
```

3. Read the merged canonical JSON on stdout:

```json
{
  "summary": "all_pass" | "some_fail" | "parse_error",
  "verdicts": [{"draft_id":"<id>","verdict":"PASS"|"FAIL","issues":["[claude+codex] ...","[grok-build] ..."]}],
  "reviewers": ["claude","codex","agy","composer-2.5","grok-build"],
  "skipped": {"<reviewer>":"<reason>"}
}
```

- **Merge rule:** a draft is FAIL if ANY reviewer flagged it FAIL; PASS only if every responding reviewer passed it. Issues are clustered across reviewers, each prefixed `[r1+r2+...]`.
- **Graceful degradation:** a reviewer that quota-/auth-fails or times out lands in `skipped` (not `parse_error`); the rest still produce a verdict. All reviewers unusable → `summary:"parse_error"`, exit 2.

### Caller responsibilities (not the binary's)

- `all_pass` → proceed to user review. `some_fail` → revise drafts using the cited issues and re-run. `parse_error` → fix inputs or escalate.
- **Never surface FAIL drafts to the user** — they should see only PASS-grade artifacts.
- **Loop protection:** `audit` is a single-call primitive with no memory. If the same FAIL signature recurs 3× across revise/re-audit cycles, stop and escalate to the human (likely the rule is wrong, not the draft).

## Mode-specific guidance

### plan mode

- Most analogous to `/codex` consult mode but iterative. Use this when a plan needs more than one critique round.
- The plan file IS the working state. Every accepted edit is applied in place.
- Convergence here is cheap (no smoke checks). Soft-convergence usually triggers in 2-3 rounds.

### implement mode

- Treat the active plan file (if any) as the contract. Convergence ≠ "code is perfect" — it's "code matches the plan and all reviewers see no remaining substantive issues."
- Auto-apply only when the edit is a single-function change with no API surface impact. Anything that touches public types, public functions, or dependency files (`go.mod`, `Cargo.toml`, `package.json`) requires preview confirmation.
- Run the smoke check (build + tests) every round. Two consecutive smoke-check failures exit to deadlock.
- Pair this with `/converge verify` afterward — implement convergence does not guarantee adequate test coverage.

### verify mode

- Critique focuses on what's *not* tested or proved, not on whether existing tests pass.
- Treats the verifier toolchain as an oracle: if Gobra/Verus/cargo-test runs in the project, run it after every fix-application round and use the obligation/coverage delta as ground truth.
- Coverage threshold is taken from the project's CI config if available; otherwise prompt the user.
- All reviewers are explicitly told that "100% line coverage" is a weak signal alone — they must propose property tests, edge cases, and adversarial inputs, not just trivial branch tests.

### review mode

- Read-only with respect to the working tree. The deliverable is `REVIEW.md`.
- Round 1 is each reasoner's independent diff review. Subsequent rounds are them critiquing each other's findings — letting one side overrule the other when its evidence is stronger.
- A finding survives to `REVIEW.md` only if it has explicit evidence (file:line) and was not conceded by the originator in a later round.
- Output verdict is `APPROVED` (all reviewers converged with no critical findings), `APPROVED_WITH_NITS` (only minor findings remain), `CHANGES_REQUESTED` (≥1 critical/major finding), or `BLOCKED` (deadlock the user must arbitrate).

## Failure modes & edge cases

- **Codex auth error:** stop, tell user to run `codex login`. Artifact is left at whichever round-state it reached; LOG records the partial run.
- **Plan file or diff is huge (>50KB):** warn the user; codex exec input has practical limits. Offer to converge on a subset (one section, one package, one file).
- **All reviewers converge in round 1:** still write the LOG with the single round; this is a successful no-op review.
- **Same issue keeps reappearing:** that's a deadlock — surface it.
- **Malformed JSON from one side:** `bin/converge validate-critique` will list the schema violations. Retry once with a stricter "Respond with JSON conforming to the schema, no prose" prompt; if still malformed, treat that side's verdict as `converged` for that round and let the other side's critique drive — note in LOG.
- **User interrupts mid-round:** the artifact is always in a consistent post-edit (and post-smoke-check, for implement/verify) state at round boundaries. Resume with `/converge <mode>` — the LOG tells you where you left off.
- **Implement mode breaks the build twice:** stop, surface the last passing state and the failing diff. Don't keep iterating on a broken tree.
- **Review mode and the diff is empty:** no work to do; tell the user.

## Watching a running session

The status snapshot is updated at every phase boundary. To peek mid-run, the user (or another agent) can:

```bash
bin/converge status show <session-id>          # one-shot
watch -n 2 bin/converge status show <session-id>   # polling

# Or follow the file directly:
tail -F "$(bin/converge status path <session-id>)"
```

The session id is `$$` of the prompt that started the run.

## What this skill is NOT

- Not a way to launder a bad artifact into a "verified" one. Reviewers agreeing means they don't see further problems within their training, not that the artifact is correct.
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
