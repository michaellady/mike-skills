---
name: port-optimize
description: Use when turning the literal Rust transliteration into the real port — value-type core with the lock lifted into a trusted shim, flattened representations, primitive collections, the memory and CPU wins — and to *prove* each optimized unit equivalent to its literal counterpart under Kani (bounded, on the byte-identical shipped source, with planted-defect controls required to fail), plus panic-freedom and the named invariants. Phase 7 of port-jvm-to-rust. Triggers — "optimize the port", "prove the optimization didn't change behavior", "Kani equivalence", "lift the lock", "value core", "negative controls", "the memory-saving representation", "second-stage port".
user_invocable: true
---

# port-optimize

This is the only phase where behavior-preserving code is *changed*, and therefore the one place a formal check earns its cost. `Literal ≡ Optimized` is a **same-language** equivalence problem, and Kani can check it: symbolic inputs, `assert_eq!(literal_op(x), optimized_op(x))`, and `abs(optimized_state) == literal_state` across operation sequences — bounded, bit-precise, machine-checked, on the actual shipped source. The proof sits exactly on the step that introduced the risk, and the claim it licenses is precise: *proven equivalent (bounded, Kani) to a literal transliteration that agrees with the original on N traces covering M% of reachable branches.*

Kani over Verus, deliberately: bounded but over shipped code, zero-annotation panic-freedom, no hand-written twins to drift ("23 verified" decomposing to one real property was a twin-drift result). Verus only for pure value cores where you want unbounded proofs and can afford the annotations.

Outputs: `rust/` (the shipped workspace), `rust/verify/kani/` (harness crate: `real.rs`, `spec.rs`, `mutants.rs`, `negative_control.rs`), `.port/optimize.md`.

## When to Use

Use when:
- `rust/literal/` is green on the public tier and the hidden-tier run is recorded.
- A representation must change (memory-bound targets) and the per-method oracle no longer applies.
- Anyone wants to say "formally verified" about any part of the port — this is the only phase that can license it, per property, with bounds.

Do NOT use when:
- The literal port is not green. An optimization of something that does not yet match the original has nothing to be equivalent to.
- The change is a redesign (new behavior). That is a different project.

## Inputs

- `rust/literal/`, `.port/literal.md`, `.port/model/` (the value-type reference model is often the starting point for the core)
- `.port/qualify.md` (which dimension the buyer is paying for — decides what to optimize)
- `.port/catalogue.json` (defects to plant as controls)

## Workflow

### 1. Apply the lock-lifting rule to the architecture

Verified core = pure value types, `step(state, req) → (state', resp)`; the lock, the I/O, the async runtime, the wire codec live in the **trusted shim**. Write the TCB boundary down: which crates the verifier reads, which it does not, and — from `port-surface` — which surfaces are therefore outside every perimeter (wire-level equivalence over a decode boundary the verifier never reads is *unstatable*; the reachable claim is over the decoded operation alphabet). Record the **TCB delta** relative to the Java: properties that moved *out* of the TCB (Java runtime checks now type-system guarantees — claim them) and *into* it (JMM guarantees now living in tokio — disclose them).

### 2. Optimize unit by unit, in the same dependency order

For each unit: the optimized version, plus an abstraction function `abs: OptimizedState → LiteralState` where representation changed. For memory-bound work the unit is the **module with a representation boundary** — a whole data structure at once, verified at its interface, not method-by-method inside it. Memory work is also where `unsafe` creeps in (arenas, `MaybeUninit`, index maps); the moment it does, Miri and ASan stop being optional and `#![forbid(unsafe_code)]` on every crate that can keep it is the default.

### 3. The Kani lane — one crate, four modules

Do not copy code into the harness. A verification-only crate whose `[lib] path` points at the shipped `src/lib.rs`, so `cargo kani` compiles the byte-identical production source in place (drop `rust-version` in the view if Kani's toolchain needs it — it is a policy field with no effect on generated code).

- **`spec.rs`** — predicates stated **once**, as derived facts (from the ledger, the RFC's grammar, the original's observed vocabulary), never a restatement of the implementation's `if` chain. *A spec that mirrors the control flow proves only that the code equals itself.*
- **`real.rs`** — harnesses over the shipped functions. Every symbolic input carries an explicit bound in the harness name and doc comment. Three harness families: **equivalence** (`literal_op(x) == optimized_op(x)`, and `abs(opt_step(s, x)) == lit_step(abs(s), x)` for sequences), **panic-freedom** (zero annotations, every unit), and **invariants** (the short named list from `port-model`'s TLA+ spec, as postconditions).
- **`mutants.rs`** — single-defect copies of the verification targets, one per spec clause, drawn from `.port/catalogue.json` translated to Rust edit shapes plus the curated operators. Never linked into a shipped artifact.
- **`negative_control.rs`** — the same spec applied to each mutant. **Every `nc_*` harness is required to FAIL.** One that reports SUCCESS invalidates the corresponding positive claim: the spec, the bound or the harness cannot see the defect. Where two properties overlap, add an expected-success control to show which is load-bearing (XOR with any per-index key is an involution, so the involution property is blind to key misalignment — the control that proves which masking property carries the weight).

### 4. State the bound, every time

BMC proves a property *up to* its bound. The only harnesses exhaustive over their domain are those whose symbolic input is small (a `u16` close code covers all 65,536). Unwinding bounds on loops are part of the claim. Write each bound into the harness name and into `.port/optimize.md`; the ceiling report quotes them.

### 5. Concurrency on the shim

`loom` / `shuttle` over the shim's action alphabet with declared bounds; label the result **bounded systematic testing, never proof**. Real preemption timing, weak-memory effects and OS scheduling are outside it, and say so. If the Java's own guarantees mattered on the surface, `jcstress` on the original tells you what they were.

### 6. Performance and memory as goldens

The buyer's dimension from `port-qualify` is a **pass/fail gate**, not a report: same workload, JMH vs criterion, JOL vs `dhat-rs`, peak RSS, bytes-per-entry, allocation counts; for memory-bound targets a 24-hour soak with an RSS-slope assertion under the chosen allocator (fragmentation is the #1 way a memory port disappoints — Java's collectors compact, Rust's allocator does not). A byte-exact port that is 8% faster is a failure you must be able to detect here.

### 7. Re-run the corpus at the module boundary

Per-method corpora no longer apply where representation changed; the module-boundary **sequence** corpora from `port-capture` do. Public tier green, then the hidden tier once from an independent context.

## Gate

Every unit: equivalence harness green with bound stated, panic-freedom green, every `nc_*` control **failing** (read the per-harness verdicts — a runner that says "all harnesses ran" has said nothing), sanitizers clean where `unsafe` exists, performance gate passing. **Canary is built in:** the negative controls. Plus clean-substitution — run the control runner with the *real* function in a mutant slot; it must fail for "caught nothing".

## Common Mistakes

- **Specs that restate the code.** Then real and mutant both verify and the negative controls are what tells you. Write derived facts.
- **Twins.** Any contract on a copy of the shipped function is a contract on a copy. The path-shim crate exists so nothing is copied.
- **Quoting "N verified".** Kani counts harnesses; ask what each reaches. REFERENCE § 3, all four questions.
- **Optimizing across the surface.** Changing anything the surface contract marks byte-exact is a `port-audit` ruling, not an optimization.
- **`unsafe` without Miri.** The safety argument for the port weakens the moment `unsafe` appears; the sanitizers restore it or the ceiling report says it was not restored.
- **Sixty-second memory benchmarks.** Arena growth and fragmentation take hours to show.
