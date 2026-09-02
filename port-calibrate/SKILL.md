---
name: port-calibrate
description: Use when you need to measure whether the port's verification ladder actually catches anything — inject defects from a catalogue not derived from the reference model into both sides, run every real rung as a subprocess and read its own verdict, and produce the per-link mutation kill table with cost in launches; the number that turns "we ran a lot of checks" into evidence. Phase 9 of port-jvm-to-rust. Triggers — "how good is each rung", "kill table", "mutation score for the port", "calibrate the ladder", "did the corpus add anything over the tests", "what does each verification layer buy".
user_invocable: true
---

# port-calibrate

Every earlier phase produced a green result and a canary showing it *can* fail. This phase asks the quantitative question: **for each link in the chain, what fraction of realistic defects does it catch, and at what cost?** Without it the ceiling report is a list of things that were run. With it, each link has a number, and the weakest number is the headline.

Two disciplines make the table mean something, and both were learned the expensive way. **The catalogue must not be derived from the reference model** — mutants injecting violations of what `S` pins, scored by a corpus generated from `S`, produce a 100% that measures alignment, not power. And **the rig must assert that the mutant was the thing exercised** — pointing a rung at the unmutated original passes cleanly and is byte-for-byte indistinguishable in the output from the mutant surviving; that slip happened for real and would have reported every rung worthless while measuring the original N times.

Output: `.port/calibration.md`.

## When to Use

Use when:
- Phases 2–8 are green and each has a canary record.
- Any rung's value is being argued from intuition ("the differential lane is expensive and finds nothing").
- Before `port-ceiling` — the ceiling quotes this table.

Do NOT use when:
- The catalogue has only contract-derived entries. Run `port-audit` first; a table from a contract-derived catalogue is a selection effect with a decimal point.

## Inputs

- `.port/catalogue.json` (provenance-labelled), `.port/corpora/`, `rust/literal/`, `rust/`, `rust/verify/kani/`, `.port/model/`, `.port/link-harness/`
- The rungs as runnable commands with a documented verdict sentence each

## Workflow

### 1. Derive per-language edit shapes from the catalogue

The catalogue records *mechanisms*, because the same defect needs a different edit per language (and two properties can sit on opposite sides of the TCB boundary in different corners — a property enforced by Java's type system may be a runtime check in Rust, or vice versa; record which). For each entry: the exact source literal and occurrence index in the Java, in `rust/literal/`, and in `rust/`. Mutants are **data** (a manifest), never control flow; adding a corner is a manifest edit.

### 2. Content-address and verify injection

Every mutant is a copy in a scratch directory — never written inside the real trees — named `impl@<hash>`. **`mutate verify` runs immediately before any sweep**: apply, diff, confirm the edit landed at the declared occurrence count. A drifted anchor injects nothing, and every rung "kills" an un-injected mutant (kill recorded against a hash so drift is visible, not silent). Anchors drifted twice in one session in the source projects.

### 3. Guard the exercised target

The registry names both `impl@<id>` and the untouched `impl`. A rung invocation must carry the `@<id>` suffix *and* the rung's working directory must be the mutant copy; the runner refuses otherwise. This is the guard against measuring the original.

### 4. Pristine must pass both judges before any mutant runs

Run every rung on the unmutated copy first; all green (read the verdict sentences). A rung red on pristine is not calibrating anything.

### 5. Run the real rungs, one mutant at a time, reading their own verdicts

Nothing here reimplements a rung — a calibration that measured a reimplementation would be measuring itself. Per mutant, per rung, as a subprocess: R0 replay (per-method and whole-surface), R1 differential, R2 property/metamorphic (the only rung that never consults the model — it is insurance against the model being wrong, and this table prices it), Kani equivalence + negative controls, TLC trace validation, interleaving search where applicable, and — separately — the unit test suite, so the corpus's *independent* contribution is visible. Record: killed / survived / **unreached** (the mutant is on a path no input exercised — a `port-alphabet` finding, not a rung failure) / not-applicable (the rung cannot see this class by construction — a sequential rung on a scheduling defect).

Judge order matters for attribution: if tests run first and kill everything, `killed_by_corpus = 0` says nothing about the corpus. Run each judge independently on every mutant; report kills per judge, not first-judge.

### 6. Cost

Measure each rung's **per-launch floor** (start a fresh process, health-wait, do nothing) and subtract it; compare rungs by **launches**, not seconds. In the source projects the property rung's own cost went *negative* after subtraction — it was process startup — and the 44× cost multiple was a property of the harness, not of property-based testing.

### 7. Write `.port/calibration.md`

Table per link (schema in `port-jvm-to-rust/REFERENCE.md` § 7), by corner, by catalogue provenance. **Lead with the caveat**: which rows share a source with the catalogue and by how much the 100%s are alignment. Then the three readings that transfer:

- a cheap conformance rung can dominate an expensive one because the corpus already absorbed the expensive rung's discoveries — a discovery rung's steady state is quiet, and *quiet and blind look identical from the kill column*; judge the differential lane on what it found, not its steady-state rate
- the property rung is usually the weakest and most expensive, and is still kept because it is the only rung that survives the model being wrong
- an `unreached` column that is not empty is the alphabet audit's to-do list

## Gate

Every mutant verified injected; pristine green on every judge; every rung's verdict read from its own output; per-judge attribution; costs floor-subtracted; the caveat paragraph present. **Canary:** include one mutant whose anchor is deliberately drifted — `mutate verify` must refuse the sweep. And one rung invocation without the `@<id>` suffix — the runner must refuse.

## Common Mistakes

- **A catalogue from the contract.** Provenance is the column that decides whether the table is informative.
- **First-judge attribution.** `killed_by_tests: 71, killed_by_corpus: 0` is unreadable until each judge runs alone.
- **Counting unreached as survived.** It is a coverage finding.
- **Dropping the differential rung because its steady-state kill rate is zero.** It is the mechanism that finds the next alphabet gap.
- **Seconds instead of launches.** Process startup dominates; the comparison is meaningless until the floor is subtracted.
- **Skipping `mutate verify`.** Twice in one session.
