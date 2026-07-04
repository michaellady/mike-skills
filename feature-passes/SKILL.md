---
name: feature-passes
description: Use when starting any substantial feature — the before/during/after development lifecycle: interview rounds, parallel blind-spot/references/reality-probe passes, a knowns matrix folded into the plan, an implementation-notes log during the build, and a compounding step after (explainer, quiz, memory/skill extraction). Triggers — "feature passes", "run the pre-build passes", "blind spot pass", "knowns matrix", "interview me about this feature", "compound the learnings".
user_invocable: true
---

# feature-passes

The before/during/after lifecycle for any substantial feature. The failure modes it exists to kill: a plan built on the author's assumptions instead of the user's decisions; design questions answered by opinion when production data could answer them by measurement; deviations during the build that silently overwrite locked decisions; and a session's hard-won learnings evaporating because nobody wrote them down as skills/memory. Every pass below is cheap relative to rebuilding a feature the user didn't ask for.

## BEFORE — all of this happens in plan mode, before ExitPlanMode

### 1. Interview the user

2-3 AskUserQuestion rounds, 3-4 questions each. Options carry previews for anything visual (a mock row, a sample sentence, a naming candidate) — never ask the user to imagine what an option looks like.

Each round narrows:
- **Round 1 — shape.** What is a row/unit? Who is it for? Where does it surface?
- **Round 2 — mechanics.** Scope boundaries, flows, sequencing, what's explicitly out.
- **Round 3 — edges.** Day-one experience (empty states, first-run), naming, deadline/demo coupling.

Lock every answer into a **decisions table** in the plan (decision | chosen option | round). These are LOCKED — later passes may challenge them, but only explicitly (see step 3), never by silent drift.

### 2. Parallel passes — launch as agents in ONE message

Four independent agents, all launched in a single message so they run concurrently:

- **(a) TECHNICAL blind-spot** — an adversarial reviewer attacks the draft plan against the *actual code*. Every finding needs file:line evidence and a concrete amendment to the plan text. "This might be a problem" without a location is not a finding.
- **(b) PRODUCT blind-spot** — attacks the plan's emotional register, audience fit, the demo moment (what's the 10-second wow?), delivery surface, gating/timeline coupling, and success signals (how will we know it landed?).
- **(c) REFERENCES** — web research resolving the design unknowns from provider docs, plus a competitor scan: what is table stakes (users will assume it) vs open ground (differentiation).
- **(d) REALITY PROBE** — measure the actual deployed/production data the design assumes. Read-only only: API GETs, log queries — CloudWatch Logs Insights works as a database when the DB is walled off (see probe-before-wire's "Logs as the read-only database"). The probe converts "will this be a firehose or a dead feed?" class questions from opinion to measurement — rows/day, churn rate, field shapes, real cardinalities.

### 3. Fold results into plan v2 with a KNOWNS MATRIX

The plan gains a `## Knowns matrix` section:

| Quadrant | Contents |
|---|---|
| **Known-knowns** | Verified facts, each with its evidence (a probe result, a file:line, a doc URL) |
| **Known-unknowns** | Tracked open questions, each with a resolution path + an owner |
| **Unknown-knowns** | Things the docs/repo already knew that the draft plan ignored — the blind-spot passes' harvest |
| **Unknown-unknowns** | Mitigations, since you can't list them: converge-iterate the plan, the impl-notes log, a live shakedown, a post-ship signal review |

**Flag explicitly any amendment that modifies a decision the user locked in the interview — never silently override.** Present the conflict ("pass (a) found evidence against locked decision D3") and let the user re-decide.

### 4. Only then ExitPlanMode

CRITICAL: build workflows launched while plan mode is active return read-only specs, not code — **subagents inherit plan mode.** ExitPlanMode first; if a build run already went out under plan mode, launch a FRESH run (never resume — the cached results are the read-only ones).

## DURING

- **Implementation-notes log.** Keep `docs/implementation-notes/<feature>.md` (or compile it from the build agents' structured deviation reports): every deviation from the plan gets one line of *why*. Don't stop to re-plan unless a deviation invalidates a locked decision — then it goes back to the user, not to your judgment.
- **Every build lane runs build → INDEPENDENT verify → fix loop.** A separate agent re-runs the gate itself in its own checkout and adversarially audits the work. A builder's self-reported green **never** counts as verified. Max ~2 fix rounds, then escalate to the human — a lane that won't converge in 2 rounds has a spec problem, not an effort problem.

## AFTER — the compounding step (this is not optional)

1. **Pitch/explainer for stakeholders.** What shipped vs what was asked, the judgment calls made along the way, and what the feature can/can't *ever* do and why (the architectural ceiling, stated plainly).
2. **Quiz.** 3-5 questions answerable only against the *deployed* thing — not the plan, not the code. Pass = the build matches the locked decisions. The quiz doubles as an onboarding/support FAQ seed.
3. **COMPOUND.** Sweep the session for:
   - **(a) caught-once traps** → memory files
   - **(b) repeated procedures** → new or extended skills
   - **(c) deterministic orchestration you hand-wrote** → parameterized Workflow scripts
   - **(d) config/transport fixes** → code (Makefile / CI)

   Apply the primitive test to sort each item: judgment → prompt, transport → code. **End the session by actually creating them, not just listing them** — a bullet list of "learnings" that never becomes a file compounds nothing.

## Relationship to other skills

- **converge** — the plan-v2 audit and the diff audits during the build both run through `/converge audit`; "converge-iterate the plan" (audit → fold → re-audit until no critical/high) is an unknown-unknowns mitigation.
- **probe-before-wire** — the REALITY PROBE pass is its design-time projection: verify the data assumption against reality before the plan bakes it in.
- **primitive-test** — the sorting function for the COMPOUND step.
