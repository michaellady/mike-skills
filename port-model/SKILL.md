---
name: port-model
description: Use when a JVM-to-Rust port needs its total, deterministic, executable reference model — the porting contract that a TLA+ spec cannot be — plus the decision ledger that closes every question the abstract model leaves open, and the mechanical link check (with canary) that the reference model refines the invariant spec. Phase 5 of port-jvm-to-rust. Triggers — "reference machine", "S_obs", "executable spec of the Java", "decision ledger", "DECISIONS.md", "what does the TLA+ leave open", "refinement check", "tlclink".
user_invocable: true
---

# port-model

A TLA+ model underdetermines an implementation. Two implementations can both refine it and disagree — on a four-endpoint toy that gap was eleven recorded decisions, including one where the model's unordered conjunction genuinely did not say which error `follow(eve, eve)` returns. On a real library it is hundreds. And transitivity (`J ⊨ S ∧ R ⊨ S ⟹ J ≡ R`) holds only when `S` **determines the output for every input**. So the porting contract is not a spec; it is a **total, deterministic, executable reference model** at the observable-surface boundary, with every open question closed *explicitly and on the record*.

The invariant spec (TLA+) is a separate, smaller object with a separate role: it says what must be *true*, it is model-checked, and the reference model is mechanically shown to refine it. Two objects, two roles. Specula's phase 2 is a decent source of candidate invariants; the running original is the only source for the model.

Outputs: `.port/model/` — the reference model (as code), `DECISIONS.md`, the TLA+ invariant spec and config, the link checker and its canary record.

## When to Use

Use when:
- Corpora, alphabet and audit exist and the port has not started.
- Someone proposes "porting from the spec" — this phase is why that is the wrong direction, and what to build instead.
- Two implementations "both refine the model" and disagree.

Do NOT use when:
- There is no captured corpus (the model's every branch must trace to observed behavior, not to an RFC).
- The target's observable behavior is dominated by scheduling or timing (a sequential total model cannot express it; say so in the ceiling and cover it in `port-link`'s concurrency lane).

## Inputs

- `.port/corpora/` and `.port/nondeterminism.md`
- `.port/surface.md` (reserved `D-numbers` for canonicalization rules), `.port/audit/findings.md` (reserved `D-numbers` for rulings)
- Optionally Specula's `base.tla` / `modeling-brief.md` from `port-audit` as invariant candidates

## Workflow

### 1. Choose the model's language and shape

Write it in **Rust** when the port is Rust: it doubles as the pure value core that `port-optimize` will keep, and the lock-lifting rule (§4) is then satisfied by construction. Write it in Go or Python only if the correlated-failure guard below is enforced.

Shape: a `step(state, request) → (state', response)` function that is **total** (every request has an answer, including every malformed one), **deterministic** (no clock, no randomness, no map iteration — time and ids are inputs), and **sort-free where it can be** (an append-log with an ordering invariant deletes an obligation a sort would create). Response bodies are canonical bytes.

**Correlated-failure guard.** No implementation may import the model. If the model shares a language with a port corner, that corner's differential agreement is weaker evidence than the others' by an amount you cannot quantify — say so in the ceiling. Enforce the import ban mechanically.

### 2. Derive every branch from observed behavior

For each surface and each edge-case family, the model's behavior comes from the corpus — what the pinned original *did* — never from the RFC, the docs, or what would be sensible. Reference models inherit pinned implementation semantics: recursive-depth limits, bounded line reads, invalid-Unicode rejection, *when* validation fires and what partial envelope escaped first. Those are part of a byte-compatible port's public behavior. Where the corpus is silent, the branch is a `D-number` marked `default`, and `port-alphabet` gets a probe request to make it observed.

### 3. Write `DECISIONS.md` — every row the model needed

Schema in `port-jvm-to-rust/REFERENCE.md` § 7. Rows come from four places: questions the invariant spec leaves open; canonicalization rules reserved by `port-surface`; rulings reserved by `port-audit`; and every impedance item from REFERENCE § 5 that applies (string representation, `StrictMath`, hash order, overflow, `BigDecimal` scale, exception timing, null, regex dialect, charset/locale, time). **Every impedance row is written before line one of Rust.** A row without evidence is a default, and defaults are listed separately so nobody mistakes them for observations.

### 4. Apply the lock-lifting rule

An abstraction function over state behind interior mutability is not definable — `Arc<RwLock<T>>` is exactly the shape no verifier can give a body to. The model is a **pure value type**: state in, state out. The lock lives in the trusted shim, later. Idiomatic Java (`synchronized` over mutable object graphs) ports naively to the unverifiable shape; deciding this here is what keeps the deductive rung reachable at `port-optimize`.

### 5. Prove the model's own properties

A test suite asserting: determinism (same inputs, same outputs, across runs and hosts), totality (every request shape in the alphabet has a response), purity (no I/O, no clock, no global), idempotence and monotonicity lemmas the surface implies, and every pinned `D-number` as an executable assertion. The model is not verified by anything else — it *is* what everything is checked against — so this suite plus the link check are its only mitigations. Say that in the TCB section of the ceiling.

### 6. Write the invariant spec and the link check

The TLA+ module states the properties (from Specula's brief, from the audit's Scenarios, from the surface's implied invariants). Bound it small enough for TLC. Then the **link check**: replay the corpus through the model, project each state onto the spec's variables (the projection is trusted, lossy, and *recorded* — what it drops and why), drop no-op steps, and emit a TLA+ module that `INSTANCE`s the real spec and forces TLC to walk exactly the recorded states against the spec's own `Next`. Ask TLC to prove the final index unreachable; a *violation* is a pass. Because that inversion is easy to get wrong, `--canary` runs a deliberately corrupted trace that **must fail**, and a canary that passes is a hard failure of the check itself.

### 7. Completeness canary

Perturb one output field in the model's response for one scenario; the corpus comparator must report it. Repeat for every field class on the surface. A field the comparator does not notice was never pinned, and equivalence over it was vacuous.

## Gate

Model suite green (determinism/totality/purity/lemmas/decisions); import ban enforced; link check passes *and* its canary fails; completeness canary recorded for every field class; every impedance item has a row. **The gate's own canary is step 6's `--canary` and step 7.**

## Common Mistakes

- **Deriving a branch from the RFC.** The RFC defines a space; the pinned library picked a point in it. Model the point.
- **A model that consults a clock or iterates a map.** Then it is not a function and the corpus cannot be byte-stable.
- **Burying decisions.** The ledger exists so a reader can tell which constraints come from the spec and which from here. A decision made silently is one the next reader inherits as a fact about the world.
- **Skipping the link canary.** "Violation = pass" is the kind of inversion that silently passes forever.
- **Trusting the projection.** It is lossy by design; record every drop (`D1`: text dropped; `D2`: ids dropped; `D11`: ids shifted by one) so a wrong projection cannot make an illegal trace look legal unnoticed.
- **Letting the model become the corpus's source.** Expectations *from* the model are self-fulfilling. The model is checked *against* the corpus captured from the original; it never generates the corpus's expectations — it generates *checks* (contracts, property suites) and *inputs*.
