---
name: verified-ship
description: Use when shipping a change through a gated pipeline (local verify → commit → push → CI → adversarial audit → auto-merge) and you must not let a check be claimed without being read. Turns the ship sequence into a hard READ-gate state machine — every gate's actual exit code / number / verdict must be read before the next step, and "run the audit" can never be co-scheduled with "arm the merge." Triggers — "ship this", "verified ship", "open the PR and merge", "gate this change", "ship with the discipline", "don't claim a number you didn't read", "read the audit before arming".
user_invocable: true
---

# verified-ship

A ship pipeline where the failure mode is not "a gate failed" but **"a gate's result was never read, yet the change advanced as if it passed."** That one bug wears many costumes:

- arming auto-merge in the same turn a background audit is still running (it merges before the verdict is read);
- writing a commit/PR body that says "100% coverage, audit all_pass" *before* the coverage run finished or the audit JSON was read;
- `git push` while `make verify` is still going;
- claiming "100%" off a green-looking partial run instead of the final number.

All four are the same root cause: **treating "I initiated the check" as equivalent to "the check passed."** This skill makes that conflation impossible by turning the pipeline into a state machine where each transition is gated on *reading the actual result of the previous step*, and where two specific actions are never allowed to share a turn.

This is the enforcement layer that sits on top of a project's ship skill (e.g. `dough-ship`). The project skill knows the commands; this skill knows the *order* and the *read-gates*.

## The non-negotiables

1. **READ before you advance.** After every gate, read its real output — the exit code, the coverage number, the test count, the audit verdict JSON — *in this turn*, before the next step. A green plan is not a green run.
2. **Never co-schedule "run audit" with "arm merge."** The audit is a gate, not a notification. Launch it, wait for the JSON, READ it, resolve or justify every FAIL, and only THEN arm auto-merge. A background audit + a fallback wakeup to read it is fine; arming before the verdict is the violation.
3. **Write the body from numbers you read.** A commit message / PR body may only state verification you actually ran and read this session — the exact test count, the real coverage %, only audits whose JSON you've opened. If the channel is degraded and you can't confirm a number, leave it out. (Content-integrity tooling will block an overstated PR body — correctly. Don't make it have to.)
4. **Verify before push, not after.** edit → run the gate → read exit 0 → commit → push. Pushing on a still-running or red gate just makes CI fail the merge later.
5. **Confirm the branch before you commit.** `git checkout -b` can silently leave you on the old branch (after a subagent run, a background job, or a worktree). Echo `git branch --show-current` immediately before `git commit` so the commit can't land on the wrong branch.

## The state machine

Each state may only be entered after the prior state's result was **read**. Do not batch a state with the next one.

```
S0 EDIT        →  make the change in small groups; re-grep that each edit landed
                  (a cancelled batch silently drops edits — `grep -c` the change is present)
S1 LOCAL GATE  →  run the full gate (e.g. `make verify`); READ the exit code + the coverage/test numbers
                  ── gate red?  → back to S0. Never advance on red.
S2 BRANCH      →  echo `git branch --show-current`; confirm it's the intended feature branch
S3 COMMIT      →  write the message FROM the S1 numbers you just read (exact counts, real %)
S4 PUSH        →  push; confirm remote SHA == local SHA (read it back)
S5 AUDIT       →  launch the adversarial audit (converge audit / cross-model); WAIT for its JSON
S6 READ AUDIT  →  open the verdict. summary == all_pass / no real FAIL?
                  ── some_fail / a real FAIL? → fix → S1 (re-gate) → S3 → S4 → S5 → S6. Loop until clean.
                  ── only non-blocking notes? → justify each in the PR body, then proceed.
S7 PR BODY     →  write/update the PR body from VERIFIED facts only (S1 numbers + the S6 verdict)
S8 ARM         →  only now: label + arm auto-merge. (S5/S6 and S8 must be different turns or at least
                  strictly ordered — never "run audit && arm merge" in one batch.)
S9 WATCH       →  watch CI to MERGED; on a required-check failure, investigate, don't assume.
```

The single most important edge: **S5→S6→S8 is strictly sequential.** If you ever find yourself about to arm a merge and you cannot point to the audit verdict you read this turn, stop — you're in the C3b failure (armed before reading; merged a `some_fail` with real findings; forced a fix-forward PR).

## If you already armed before reading (recovery)

`gh pr merge <n> --disable-auto` *immediately*, then go to S6. Disarming is cheap; an unreviewed merge is a fix-forward PR.

## Degraded-channel rules

- Bash stdout sometimes returns empty then flushes a turn later. The clean `rev-parse` / exit-code values are authoritative over garbled mid-batch output. Re-probe with one small command rather than re-running destructive work.
- A denied or failed call in a parallel batch **cancels the whole batch** — including Edits/Writes that would have applied. After any cancellation, `grep -c` / `git status` / `git diff` the actual files before claiming a fix landed (you may have "fixed" the same bug three times, each in a cancelled batch).
- Keep PR-creating / merge-arming / destructive git calls OUT of big batches, so one deny doesn't strand them or fire them half-configured.

## Untangling a mis-branched commit (force-push is often blocked)

If a commit landed on the wrong branch: cherry-pick it onto the correct branch; `git reset --hard <good-sha>` the wrong branch locally; then — since the remote needs rewinding and force-push may be classifier-blocked — close the contaminated PR, `git push origin --delete <branch>`, and re-push the corrected branch fresh (a new-branch push, not a force). Keep independent changes on separate branches from the start.

## Relationship to other skills

- The **project ship skill** (e.g. `dough-ship`) owns the commands, the cover gate, the required checks. `verified-ship` owns the read-gate ordering around them.
- **converge** (`audit` mode) is the S5 primitive. This skill is what makes you *read* its output before S8.
- **maximize-verification** decides *what* the gate should check; `verified-ship` makes sure the gate's verdict actually governs the merge.
