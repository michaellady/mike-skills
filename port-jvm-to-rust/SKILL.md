---
name: port-jvm-to-rust
description: Use when porting a legacy Java / Kotlin / JVM codebase to Rust and the port must be behavior-preserving with the highest defensible confidence — a calibrated evidence ladder, not a "verified" slogan. Orchestrates the port-* phase family (qualify → surface → capture → alphabet → audit → model → literal → optimize → link → calibrate → ceiling). Triggers — "port this Java to Rust", "JVM to Rust port", "bug-for-bug port", "behavior-preserving port", "port and prove it", "take an impression of this Java", "rewrite in Rust without changing behavior", "verified port", "drop-in Rust replacement".
user_invocable: true
---

# port-jvm-to-rust

Orchestrates a **behavior-preserving port of a JVM codebase to Rust** whose output is not just Rust code but an *evidence bundle* an auditor can check line by line: what the original does (a total executable reference model plus a decision ledger), how much of it the port reproduces (a differential ladder with a measured coverage denominator), which properties are machine-proved (Kani, bounded, with negative controls), and how good each check actually is (a mutation kill table against real defects). The port depreciates; the characterization outlives it.

This skill is the **procedure and the gate order**. Each phase is its own skill with a mandatory input file and a mandatory output file, so the methodology is *traversed*, never remembered. The full model — three failure modes, the confidence chain, claim vocabulary, the JVM→Rust impedance checklist, polarity defect classes, artifact schemas — lives in [REFERENCE.md](./REFERENCE.md). Read it once before the first phase.

## When to Use

Use when:
- Porting Java, Kotlin, Scala or any JVM artifact to Rust where "same inputs → same outputs" is the requirement (maintenance-mode legacy, cost/performance-driven rewrites, PE-style "don't fix what isn't broken").
- The buyer will ask "what did you check" and needs an answer that survives an auditor.
- Someone says "verified port" and you need to turn that into claims that can actually be defended.

Do NOT use when:
- The goal is a *redesign* in Rust (new behavior is welcome) — use `maximize-verification` directly on the new code with the old as a differential oracle, and skip the model/ledger machinery.
- The target is a greenfield system with no original to characterize.
- The original is not runnable (no jar, no build, no tests) — then there is no oracle and the whole ladder has nothing to stand on; say so.

## The three failure modes (why the phases exist)

A port is wrong in exactly three ways, and they need three different instruments. Most "verified port" efforts cover only the first.

| Mode | What went wrong | Instrument |
|---|---|---|
| **1. Divergence** | Rust does something Java didn't | differential + conformance vs the pinned original (`port-capture`, `port-literal`) |
| **2. Faithful defect** | Rust matches Java, and Java was wrong — the ladder *certifies* bug-for-bug | defect audit of the original (`port-audit`) → explicit preserve/fix decision in the ledger |
| **3. New dimension** | Rust introduces a defect the oracle cannot express — concurrency, memory, panics, timing | Kani + loom/shuttle + Specula-style interleaving search on the Rust (`port-optimize`, `port-link`) |

## Phase order and handoffs

Every phase reads the previous phase's file and writes its own under `.port/` in the target checkout. **Do not skip a phase; do not start a phase whose input file is missing.** Each phase's gate must be shown *able to fail* (a canary) before its green result counts.

| # | Skill | Reads | Writes | Gate |
|---|---|---|---|---|
| 0 | [port-qualify](../port-qualify/SKILL.md) | the JVM checkout, a profile | `.port/qualify.md` | the JVM is actually the cost; go/no-go |
| 1 | [port-surface](../port-surface/SKILL.md) | qualify | `.port/surface.md`, `.port/deps.md` | observable surface signed; every dependency has a disposition |
| 2 | [port-capture](../port-capture/SKILL.md) | surface | `.port/corpora/`, `.port/nondeterminism.md` | two captures diffed; every divergence classified; empty stub fails every scenario |
| 3 | [port-alphabet](../port-alphabet/SKILL.md) | corpora | `.port/alphabet.md` | branch-coverage denominator on the *Java*; unreached list published |
| 4 | [port-audit](../port-audit/SKILL.md) | surface, alphabet | `.port/audit/`, `.port/catalogue.json` | every defect has a preserve/fix ruling; security defects escalated |
| 5 | [port-model](../port-model/SKILL.md) | corpora, audit | `.port/model/` (reference machine, `DECISIONS.md`, TLA+, link check) | model is total + deterministic; refinement check has a canary |
| 6 | [port-literal](../port-literal/SKILL.md) | model, corpora | `rust/literal/`, `.port/literal.md` | per-method R0 byte-exact; R1 clean; each with canary |
| 7 | [port-optimize](../port-optimize/SKILL.md) | literal | `rust/` (shipped workspace), `rust/verify/kani/` | `literal ≡ optimized` per unit under Kani; every `nc_*` control fails |
| 8 | [port-link](../port-link/SKILL.md) | optimize, model | `.port/link.md`, traces | recorded Rust traces admitted by the TLA+ model; canary trace rejected |
| 9 | [port-calibrate](../port-calibrate/SKILL.md) | everything | `.port/calibration.md` | kill table per link, catalogue not derived from the model |
| 10 | [port-ceiling](../port-ceiling/SKILL.md) | everything | `.port/CEILING.md` | the weakest link is the headline |
| ∗ | [port-learn](../port-learn/SKILL.md) | every gate record; the ceiling | `.port/learn/F*.md`; a PR against this skill family | every finding binned; rediscoveries move the rule into the step |

`port-learn` is the **standing phase**: it runs at every gate (two minutes when nothing happened) and in full after `port-ceiling`, and it is the only phase allowed to edit this family — with a cited finding, at the step where the rule fires, never in a notes directory.

Phases 0–5 characterize the original and never touch Rust. Phase 6 is the only place code is *translated*; phase 7 is the only place it is *changed*. That separation is deliberate: the agent that translates must not also be the agent that decides what "equivalent" means (see `maximize-verification` Step 3 — correlated failure).

## Standing rules (apply in every phase)

1. **No gate is decided by an exit code.** Read the tool's own output. A piped exit code is laundered.
2. **No gate is trusted until it has been shown to fail.** Every rung has a canary — an injected defect *and*, for any proof, a negation or planted-defect control. A check that cannot fail proves nothing.
3. **The implementing agent never touches the oracle.** Corpora, the reference model, the ledger, the canonicalizer and every abstraction function are read-only to whoever writes Rust, checksummed, and re-run in an independent context. The canonical cheat is not weakening a test; it is regenerating a golden file or canonicalizing a difference away.
4. **Never re-baseline to make a difference disappear.** A corpus shift is a finding about behavior, filed in the ledger, never absorbed.
5. **Security defects are never silently preserved.** Bug-for-bug is the default for quirks. A defect with a security consequence found by `port-audit` is always an explicit, recorded ruling by the owner — even when the ruling is "preserve for compatibility."
6. **Every count is audited before it is quoted.** "N verified", "N killed", "N passed" — ask what would have to be true for the number to be wrong, and check that instead (REFERENCE.md § Counts).
7. **A recorded blocker is a measurement with a timestamp.** Re-run it before building on it.
8. **Pin the model, not just the tools.** The agent, model version and effort are part of the receipt alongside every toolchain digest. A finding can be a property of a model build.
9. **The claim vocabulary is fixed.** "Formally verified" only for the Kani-proved properties and the model refinement check, named individually. The equivalence lane is "calibrated differential assurance". The port claim is "behavior-preserving on the reachable surface". Never "1:1", never "absolutely correct". (REFERENCE.md § Claims.)
10. **Every gate ends with `port-learn`.** File what surprised or cost, bin it, and if a rule already existed, move it into the step that needed it. A rediscovery is data about where the family's rules fail to reach, not an embarrassment.

## How to run it

1. Read [REFERENCE.md](./REFERENCE.md) once.
2. Run `port-qualify`. If it says no-go, stop and deliver *that* — it is a real result.
3. Run phases 1–10 in order. At each phase boundary, confirm the input file exists and the previous gate's canary was recorded, then invoke the next skill.
4. At phase 6 and 7, hand layer selection to `maximize-verification` (it picks which independent checks the unit admits) and any agent-written comparator to `converge audit` (cross-model review of the thing that decides "equal").
5. After each gate, run `port-learn` (short form). After `port-ceiling`, run it in full; its PR against this family is part of the deliverable.
6. Deliver `.port/CEILING.md` as the headline artifact. The Rust workspace is an attachment to it.

## Where Specula fits

[Specula](https://github.com/specula-org/Specula) is a *defect-discovery* instrument, not an assurance rung. It appears in exactly two slots, and only for targets that are stateful or protocol-shaped: `port-audit` (agent-written TLA+ over the *Java*, model-checked, violations reproduced → defects in the original → ledger rows and the mutation catalogue) and `port-link` (its trace-validation phase — drive TLC through traces recorded from the *Rust* — which is the cheapest obtainable link between the port and the invariant model). Its output never enters the assurance column, never retires an obligation, and a green Specula run licenses nothing.

## Common Mistakes

- **Porting from the spec.** A TLA+ model underdetermines an implementation (the reference model's `DECISIONS.md` exists because of exactly this). The Java source is the generative input; the model and corpus *adjudicate*. Generate the *checks* from the model, never the code.
- **One oracle at the API boundary.** Then the port is one enormous diff with no localization and a coverage bounded by the top-level alphabet. `port-capture` builds per-method oracles by bytecode instrumentation so the port decomposes into independently verifiable units.
- **Optimizing during translation.** Representation changes break the per-method oracle. Translate literally first (`port-literal`), then optimize with a Kani equivalence proof back to the literal (`port-optimize`). The proof goes where the risk is.
- **Treating the Java as a function.** Hash iteration order, `identityHashCode`, default charset/locale/timezone, `Math.*` variance, scheduling. Two captures on two JDK builds, diffed, *before* any golden file is trusted.
- **Counting.** "23 verified" that decomposes to one real property; mutants "killed" by drifted anchors; obligations verified over unreachable code. See REFERENCE.md § Counts.
- **Process weight.** A signed-decision governance stack for a 4,000-line core is a demonstration, not a template. Keep the gates, drop the ceremony: a goal file, a story queue, the rig, the ledger.
