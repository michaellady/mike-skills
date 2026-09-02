---
name: port-audit
description: Use when the defects in the *original* JVM code must be found before porting it — the ones a behavior-preserving ladder would otherwise faithfully reproduce and certify — via git/issue archaeology, the unreached-branch list, and (for stateful or protocol-shaped targets) a Specula run; every finding becomes an explicit preserve / fix ruling in the ledger and a row in the mutation catalogue. Phase 4 of port-jvm-to-rust. Triggers — "audit the original", "what bugs are we about to port", "bug-for-bug or fix", "Specula on the Java", "defect catalogue", "preserve or fix ruling".
user_invocable: true
---

# port-audit

The differential ladder has a blind spot it cannot see from inside: **if the original is wrong, byte-exact agreement is bug-for-bug compatibility, and every rung that consults the original scores the faithfully-ported defect as a pass.** For maintenance-mode legacy code — old, load-bearing, understood by nobody — this is the dominant risk, and it is the one instrument-free rung of the ladder.

This phase is the instrument. It produces two things: a list of defects in the original, each with an explicit **ruling** (preserve for compatibility, or fix and break compatibility in a named way — the owner decides, never the port agent); and a **defect catalogue drawn from a source other than the contract**, which is what makes the later kill table informative instead of a selection effect.

Outputs: `.port/audit/findings.md`, ledger rows reserved for each ruling, `.port/catalogue.json`.

## When to Use

Use when:
- The corpus and alphabet audit exist and the port has not started.
- A finding from `port-capture`'s nondeterminism census or `port-alphabet`'s fuzzing looks like a real defect.
- The target has a git history, an issue tracker, or a CVE history worth mining.

Do NOT use when:
- The target is a redesign (then defects in the old system are requirements, not rulings).
- Specula is being reached for on a target that is not stateful or protocol-shaped — the archaeology still runs; skip the Specula slot (see § Specula).

## Inputs

- `.port/surface.md`, `.port/alphabet.md`, `.port/nondeterminism.md` (real-defect rows)
- The original's git history, issue tracker, release notes, CVE/advisory history
- A Specula install (optional, gated by target shape)

## Workflow

### 1. Bug archaeology

Mine the history the way Specula's phase 1 does: every fixed bug, every issue, every advisory, grouped into **Scenarios by shared mechanism** — "non-atomic persistence with a crash window", "guard missing on the re-entry path", "string comparison by reference" — not by file. For each Scenario: the mechanism, the historical instances, the code regions it lives in, and whether those regions are in the unreached list from `port-alphabet` (an unreached region with a bug history is the highest-priority probe target).

### 2. Read the unreached and the nondeterministic

Every branch on the unreached list with a "not yet reached — open" reason gets read with the question *what input reaches this, and what happens then*. Every `real defect` row in the nondeterminism census is investigated here. Every crash or hang the fuzzer found in the original is reproduced here.

### 3. The Specula slot — only for the right shape

Run Specula's pipeline on the **Java** when the target is stateful or protocol-shaped: message-passing, persistence with crash windows, lock-free structures, connection state machines, anything with a reference algorithm. It writes an agent-authored TLA+ model scoped by the Scenarios, model-checks with fault injection, and **reproduces** violations at the code level with an executed test. Take only findings with an executed reproduction (`REPRODUCED`) or a sound argument (`ENV_LIMITED`, `MASKED`); a `FALSE POSITIVE` or `PENDING REPAIR` is not a finding.

Do not run it on a sequential CRUD/batch/rules-engine target — its phase 1 will produce a brief with nothing in it at frontier-model cost. Do not read a green Specula run as evidence of anything: its "no violations → report success" is exactly the check-that-cannot-fail shape, and its model is agent-written from the code it is checking.

### 4. Confirm every candidate

Every defect gets the same bar Specula's confirmation phase uses: **reachable** through the real surface (not a fabricated pre-condition) *and* a **consequence** a real consumer observes. Reproduce with an executed test whose output is pasted into the finding. "Code audit only" is not a finding. Split the no-live-harm cases honestly: not a defect (documented/intended) vs. **masked** (real, consequence currently hidden by a safeguard — name the mask) vs. **environment-limited**.

### 5. Ruling, per finding — the owner's, not yours

| ruling | when | consequence |
|---|---|---|
| `PRESERVE` | quirk with no consequence, or consumers depend on it | ledger row; the port reproduces it; the corpus already pins it |
| `PRESERVE-AND-FLAG` | real defect, compatibility wins for now | ledger row; a named test that *documents* the defect so the next maintainer knows it is deliberate |
| `FIX` | consequence outweighs compatibility | ledger row naming exactly which corpus goldens change and why; the surface contract is amended and re-signed |
| `ESCALATE` | **any security consequence** | always the owner's explicit, dated ruling — even when the answer is preserve. Never silent. |

Reserve the `D-number` here; `port-model` writes the row.

### 6. Build the catalogue

`.port/catalogue.json`: one entry per confirmed defect, with the mechanism, the site, the minimal edit that reintroduces it (for the Rust, once it exists — the same defect needs a different edit shape per language, so record the *mechanism* and let `port-calibrate` derive the edit), and its source: `history | unreached | nondeterminism | fuzz | specula`. Add curated operators from the polarity list in `port-jvm-to-rust/REFERENCE.md` § 4 as a separate source, labelled. **The catalogue's value is that it did not come from the reference model.** Keep the provenance column honest.

## Gate

Every confirmed defect has a ruling with an owner and a date; every `ESCALATE` has the owner's explicit sign-off; the catalogue has at least one entry per Scenario with provenance not equal to the contract. **Canary:** the reproduction test for each `REPRODUCED` finding must pass (trigger the defect) on the pinned original and be *shown* failing on a scratch copy with the defect fixed — a repro that cannot distinguish the fix has not reproduced anything.

## Common Mistakes

- **Letting the port agent rule.** Preserve/fix is the owner's decision; the port agent has the incentive to preserve everything (fewer goldens move).
- **Preserving a vulnerability quietly.** Shipping a knowingly-ported vulnerability into a client's stack is the failure that ends the practice. `ESCALATE` is never optional.
- **Counting a Specula green as verification.** It is a defect-discovery instrument; silence licenses nothing.
- **Running Specula on the wrong shape.** Budget it for protocol-shaped targets; elsewhere the archaeology is the whole phase.
- **A catalogue derived from the contract.** Then the kill table measures alignment, not power. Provenance is the column that matters.
