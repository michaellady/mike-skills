---
name: port-alphabet
description: Use when "the corpus passes" needs to become a number with a denominator — measure branch coverage of the *original* JVM code under the captured corpus, expand the alphabet with coverage-guided fuzzing until it saturates, and publish the list of branches no input reaches. Phase 3 of port-jvm-to-rust. Triggers — "what can the corpus not express", "alphabet audit", "coverage denominator", "is 100% pass meaningful", "which branches are unreached", "widen the corpus".
user_invocable: true
---

# port-alphabet

**Volume is not coverage.** A differential campaign's reach is bounded by its generator's alphabet, and no number of requests widens it: 100,000 differential requests found nothing while four live divergences sat in code no request shape could reach; adding twelve shapes failed at step 8 of the first trace. "247/247 Autobahn cases" and "100k requests, no mismatch" are the same kind of number, and neither speaks to a construct the corpus cannot emit.

This phase makes the alphabet a *measurement* instead of a guess: instrument the **original** for branch coverage, run every corpus tier through it, fuzz the original coverage-guided until new branches stop appearing, and publish the unreached list. That list is the honest denominator for every kill rate and pass rate downstream.

Output: `.port/alphabet.md`.

## When to Use

Use when:
- The corpus exists (`port-capture` done) and no one can say which code it does not reach.
- Any rung reports 100% and the next question is "of what".
- Before `port-audit`, so its bug archaeology can target the unreached regions.

Do NOT use when:
- The corpus does not exist yet.
- The target is a redesign (then coverage of the *new* code is the question, and that is `maximize-verification`'s job).

## Inputs

- `.port/corpora/` (all tiers, with source labels)
- `.port/surface.md` (edge-case families per surface)
- The pinned original with a coverage agent attachable (JaCoCo)

## Workflow

### 1. Coverage of the original under each corpus source

Attach JaCoCo, replay each corpus tier and each *source* (test suite / generated / production) separately, and record **branch** coverage per method on the study surface — not line coverage (a zero-assertion test gives 100% line coverage). Report the union, and the marginal contribution of each source. A source that adds no branches over the others is not adding coverage, whatever its size.

### 2. Reach for edge-case families

For every surface, the contract enumerated `nil, empty, malformed, oversized, duplicate, interrupted, stale, upstream-error`. Map each family to the branches that handle it; report which families the corpus never enters. These are the canonical blind spots (a query string on a POST path; a percent-encoded segment; whitespace around a body; a malformed escape; a close frame with an empty payload; a fragment boundary inside a multi-byte rune).

### 3. Expand with coverage-guided fuzzing on the Java

Jazzer (libFuzzer-based, in-process, JVM) or JQF, seeded from the corpus, target = the adapter's entry point. Run until new-branch discovery plateaus (record the plateau: branches vs. executions). Every input that reached a new branch is promoted into the **generated** corpus source *as a recorded scenario with its captured expectation* — through `port-capture`'s adapter, so its golden comes from the original, never from the fuzzer's guess.

Crashes and hangs in the *original* are findings for `port-audit`, not noise.

### 4. Targeted probes for corpus-invisible boundaries

Some boundaries no fuzzer finds by coverage alone: two states that account bytes differently but produce the same output on every corpus case (CLOSING vs CLOSED), a guard whose collapse preserves all existing cases. For each surface, ask: *what plausible refactor would preserve every existing case while erasing a meaningful boundary?* Write one probe per answer. These are the probes that later distinguish a correct optimization from a lucky one.

### 5. Re-run after any repair of the oracle or model

If a reference model or adapter is fixed and the regenerated corpus is **byte-identical**, downstream passing results have not observed the changed contract. The repair needs an input that reaches the changed decision before the corpus can serve as regression evidence for it. Check this mechanically: a fix to the oracle must move at least one golden, or it is unobserved.

### 6. Write `.port/alphabet.md`

```
# Alphabet audit — <target> @ <commit>

Branch coverage of the original (study surface): <n>/<m> = <p>%
  by source: tests <a>%  generated <b>%  production <c>%  fuzz-promoted <d>%
Edge-case families never entered: <surface → families>
Unreached branches: <file:line list, grouped by method>, with a one-line "why" each
Fuzz plateau: <branches> after <executions>, seeds <k>
Targeted probes added: <list, each naming the boundary it protects>
Original crashes/hangs found: <list → port-audit>
```

## Gate

The unreached list is published and every unreached branch has a one-line reason (dead code | requires an environment the lab cannot provide | requires a construct the surface excludes | **not yet reached — open**). **Canary:** delete the highest-marginal corpus source and confirm the reported coverage drops; a coverage number that does not respond to its inputs is not measuring them.

## Common Mistakes

- **Line coverage.** It measures what executed, not what was decided. Branch coverage, always.
- **Fuzzing the Rust instead of the Java.** The denominator is on the *original*; the port inherits its alphabet.
- **Promoting a fuzzer input with a fuzzer-guessed expectation.** The golden comes from the original through the adapter, every time.
- **Reporting the plateau as "done".** A plateau is where *this* fuzzer stopped. Step 4's targeted probes exist because coverage-guided search is blind to state boundaries with identical outputs.
- **Forgetting step 5.** An oracle fix that leaves the corpus byte-identical has not been observed by anything downstream — this exact failure was written down and rediscovered thirteen hours later in the source projects.
