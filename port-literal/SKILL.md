---
name: port-literal
description: Use when producing the first-stage Rust port — a literal, structure-preserving transliteration of the JVM original (same types, same methods, same control flow, no optimization) whose only job is to be isomorphic to the Java so the per-method corpus applies with no abstraction function; drives it to byte-exact conformance and clean differential agreement, per method, in dependency order. Phase 6 of port-jvm-to-rust. Triggers — "transliterate the Java", "literal port", "first-pass Rust", "mirror the Java structure", "per-method equivalence", "port in dependency order".
user_invocable: true
---

# port-literal

The port happens in two stages, and this is the first. `Literal-Rust` mirrors the Java one-to-one: classes as structs, methods as methods, the same control flow, `Arc<Mutex<_>>` where Java had `synchronized`, `Vec<u16>` where the alphabet audit says surrogates are reachable. It is ugly and unoptimized on purpose. Its value is that `Java ≡ Literal` becomes the **easiest possible differential problem** — isomorphic structure means the per-method golden corpus from `port-capture` applies to every function with no abstraction function, the same mutants apply to both sides, and a divergence localizes to one method instead of "byte 4,812 of trace 91". The optimization, and the proof that it changed nothing, is `port-optimize`'s job.

The Java **source** is the generative input; the model and corpus **adjudicate**. An LLM is excellent at translation and bad at invention: given source, it produces Rust that means the same thing including the accidental semantics nobody wrote down; given a spec, it invents everything the spec omits, plausibly and silently.

Outputs: `rust/literal/` (a workspace that is never shipped), `.port/literal.md` (per-method R0/R1 status and the canary record).

## When to Use

Use when:
- `.port/model/` exists with its gate passed and the ledger's impedance rows are written.
- A port was started by optimizing during translation and cannot be localized — restart here.

Do NOT use when:
- The ledger is missing impedance rows (string representation, overflow policy, hash order). Translating before those are decided produces a port that must be redone.
- The target is memory-bound *and* the module's representation will change — translate the module literally anyway (it is the equivalence anchor), but expect `port-optimize` to replace it wholesale and prove the replacement at the module boundary.

## Inputs

- The pinned Java source (read-only), `.port/model/DECISIONS.md`, `.port/corpora/public/` (never hidden or sealed)
- `.port/deps.md` — which dependencies are `port` (translate them too, literally), `wrap`, `replace` (bind, and run their delta corpus), `out-of-surface`
- `.port/surface.md`

## Workflow

### 1. Fix the translation rules from the ledger, once

Before any method: string type per the surrogate decision; integer policy (`wrapping_*` everywhere Java wraps — never bare `+` on a Java `int`); `Math` → `StrictMath` port or documented deviation; `HashMap` → the ordered/fixed-hasher choice per site; `null` → `Option` with the map-missing-vs-present-null rule; exception → `Result` **preserving the throw point** (what partial output escaped first); regex dialect; charset/locale pins. Write them as a table at the top of `.port/literal.md`. Every translated method cites the rules it used.

### 2. Order the work by dependency

From the Java's call graph, leaves first. Each unit is one method (or one small cohesive class). A unit is not started until its callees are green.

### 3. Per unit: translate, then adjudicate

- Translate the unit *from the Java source*, structure-preserving. No renaming beyond Rust naming convention, no restructuring, no "improvements". A comment on each function names the Java symbol it mirrors (the compiler-derived identity from `port-surface`, not a guess).
- Run the unit's **per-method corpus**: same arguments in, byte-compare returns and thrown-exception identity. R0 = byte-exact.
- Run the **differential lane** against the live original through the adapter on generated traces reaching this unit. R1 = zero unexplained mismatches; every explained mismatch is a ledger row, never a comparator change.
- Hand the unit to `maximize-verification` for the layer set it admits (property/metamorphic, negative paths, contracts) — the *independent* checks that never consult the oracle and therefore survive the oracle being wrong.
- Any mismatch: fix the Rust, or if the Java is the one that is wrong, **stop** — that is a `port-audit` finding, not a translation bug, and the ruling decides which side moves.

### 4. Whole-surface R0/R1 when the graph is green

Replay every public-tier scenario through the assembled literal workspace via a Rust adapter speaking the same JSONL protocol as the Java one. Three-way verdict per scenario — `MATCH` (byte-exact), `TRIM` (identical modulo the ledger's whitespace rule), `DIFF` (a real disagreement, classified by kind). Zero `DIFF`. Then the differential lane over generated traces at volume, each trace against a fresh process so failures reproduce from the seed alone.

### 5. Concurrency shim, literally

Where the Java was `synchronized`, the literal port takes the same lock at the same points. This is deliberately the *unverifiable* shape; it exists so `port-optimize`'s lock-lifting has an anchor to prove equivalence against. Run `loom`/`shuttle` on it only if the Java's locking is itself on the surface (rare).

### 6. Record the canary

Plant one defect (from the catalogue, translated to this unit's shape) in a scratch copy; the per-method corpus must catch it. If it does not, the unit's corpus does not reach that mechanism — file for `port-alphabet`, and the unit is not green.

## Gate

Every unit R0 byte-exact and R1 clean on the public tier with a recorded planted-defect kill; whole-surface R0 zero `DIFF`; the Rust adapter proven able to fail (empty stub fails every scenario). Hidden-tier run happens **once**, by an independent context, after the porting agent declares done — the result is recorded, the contents are never shown.

## Common Mistakes

- **Optimizing while translating.** Every representation change here breaks the per-method oracle and moves the risk to where nothing can check it. Ugly is correct.
- **Bare arithmetic on Java ints.** The single most common silent divergence. Wrapping ops everywhere, then let `port-optimize` prove where checked arithmetic is safe.
- **Translating from the model.** The model adjudicates. The source generates.
- **Fixing a mismatch in the comparator.** The comparator is TCB and read-only to this phase. A mismatch is Rust wrong, Java wrong (→ audit), or a ledger gap (→ model). Never "canonicalize it away".
- **Seeing the hidden tier.** Once seen, it is the public tier.
- **Reporting `killed_by_tests` as corpus power.** If unit tests kill everything first, the corpus's independent contribution is unmeasured; `port-calibrate` separates them.
