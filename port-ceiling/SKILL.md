---
name: port-ceiling
description: Use when writing the headline deliverable of a JVM-to-Rust port — the ceiling report that leads with the weakest link, states every claim in the fixed vocabulary with its bound and its canary, publishes the coverage denominator, the nondeterminism census, the dependency dispositions, the preserve/fix rulings, the kill table, the cost and the performance/memory deltas, and places the port on the cutover readiness ladder. Phase 10 of port-jvm-to-rust. Triggers — "write the assurance report", "ceiling report", "what can we claim", "what did we check", "cutover readiness", "the auditor's document", "what does the port not cover".
user_invocable: true
---

# port-ceiling

The Rust workspace is an attachment to this document, not the other way round. A buyer of a mission-critical port will be asked "what did you check" by an auditor, and the answer that survives is not "formally verified" — it is a page that leads with the weakest link, names every claim with its bound, and says what the ladder cannot see. That honesty is also the commercial differentiator: "AI-verified Rust port" is a claim anyone can make; a per-link kill table with a stated selection effect is a claim only this ladder can produce.

Ceilings are **results**, not excuses: "JBMC 6.11.0 verifies `"abc".equals("abc")` as FALSE, so no string-equality obligation is dischargeable on the Java side" is a finding about the tool, recorded with its version, and it is worth more to the next reader than the obligation would have been.

Output: `.port/CEILING.md`, and the receipt.

## When to Use

Use when:
- `port-calibrate` has produced the kill table.
- Anyone asks what the port guarantees, or is about to write "verified" in a proposal, README or contract.
- At every *intermediate* milestone too — a ceiling report with empty rows is more honest than no report, and it is the file that makes the next phase's job legible.

Do NOT use when:
- Phases are missing without a recorded reason. The report has a row for each; an unrun phase is a row saying "not run — because", never a missing row.

## Inputs

Every `.port/*.md`, `.port/model/DECISIONS.md`, `.port/calibration.md`, `.port/catalogue.json`, `rust/verify/kani/` results, the benchmark and soak results, `.port/learn/` (open findings).

## Workflow

### 1. Find the weakest link and put it first

Walk the ten links (`port-jvm-to-rust/REFERENCE.md` § 1). For each: evidence, canary record, calibration number, ceiling. The **minimum** is the headline sentence, before any green result. If the weakest link is "the corpus reaches 71% of the original's branches and these 40 branches are unreached", that is the first line.

### 2. State each claim in the fixed vocabulary, with its bound

REFERENCE § 2. Per Kani harness: the property, the symbolic input bound, the unwinding bound, the negative controls that failed. Per trace-validation run: traces, sources, "empirical refinement, not proof". Per interleaving search: alphabet and context-switch bound, "bounded systematic testing". The equivalence lane: "calibrated differential assurance — kill table attached". The port: "behavior-preserving on the reachable surface: N% of branches, M scenarios across tiers, K decisions recorded".

Then the sentence that is not allowed: the report never says "1:1", "absolutely correct", "formally verified port" or "equivalent" unqualified — and it says so, so a reader who expected those words knows why they are absent.

### 3. Publish what the ladder cannot see

- the unreached-branch list and the edge-case families never entered (`port-alphabet`)
- the nondeterminism census: every field classified, and the ones declared out-of-surface
- the dependency dispositions, and for every `replace`, the characterized delta
- the surfaces outside every verification perimeter (the decode boundary; the shim; the runtime)
- the concurrency lane's limits: real preemption, weak memory, OS scheduling
- the TCB delta: properties that moved into the TCB in the port (disclosed) and out of it (claimed)
- the model's own trust: it is verified by nothing but its suite and the link check; the projection is lossy in these recorded ways; the corner that shares a language with the model has weaker differential evidence by an unquantified amount
- **the model pin**: agent, model version, effort, alongside every toolchain digest — a finding can be a property of a model build

### 4. The rulings

Every `port-audit` finding with its ruling, owner and date; every `ESCALATE` with the explicit sign-off. This is the section the client's counsel reads.

### 5. The numbers the buyer asked for

From `port-qualify`'s driver: measured speedup or memory reduction on the same workload; bytes-per-entry and RSS at steady state and its slope over the soak for memory-bound targets; cost per rung in launches; **cost per KLOC ported** and wall time per phase. Nobody publishes the last two; they are what the first meeting asks.

### 6. Cutover readiness

Place the port on the ladder and say what the next rung needs: `SOURCE_QUALIFIED → SEMANTICALLY_VERIFIED → OPERATIONALLY_VERIFIED → SHADOW_VERIFIED → CANARY_VERIFIED → CUTOVER_READY`. `SHADOW_VERIFIED` is the production-differential rung (mirror real traffic to old and new, diff) and is the only rung in the family that sees the real distribution; it is not reached in a lab.

### 7. Re-test recorded blockers before quoting them

Every "cannot be done" in the report is a measurement with a timestamp. Five of five recorded blockers checked in the source projects were stale ("no `vstd::hash_set` model exists" — it ships four). Re-run each before it justifies a ceiling.

### 8. Write `.port/CEILING.md` and the receipt

Schema in REFERENCE § 7. The receipt: every tool and its digest, every model and version, the pinned original's digest, the corpus manifest digests, the date, and the command that regenerates each number.

## Gate

The headline is the weakest link; every claim carries a bound and a canary reference; every unrun phase has a reason; every count passed REFERENCE § 3's four questions; the vocabulary is the fixed one. **Canary:** hand the report to a different model with `converge audit` and the rules "find any claim without a bound", "find any count without its four questions", "find any 'verified' not tied to a named harness". It must find nothing.

## Common Mistakes

- **Leading with the green.** The reader stops at the first line; make it the true one.
- **A ceiling as an apology.** It is a result with a tool version attached.
- **Omitting the model pin.** You pinned every tool because findings are properties of builds; the agent is a build.
- **Quoting a stale blocker.** Re-run it.
- **"Formally verified" anywhere it is not tied to a harness name and a bound.** One competent reviewer takes the whole report apart with that sentence.
