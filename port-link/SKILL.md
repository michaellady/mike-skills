---
name: port-link
description: Use when the shipped Rust port must be connected to the invariant model empirically — instrument the Rust to emit traces, drive TLC through every recorded transition against the TLA+ spec (Specula's trace-validation phase), run the interleaving search on the Rust for the concurrency defect class no sequential oracle can score, and record a corrupted-trace canary. Phase 8 of port-jvm-to-rust. Triggers — "does the Rust refine the model", "trace validation", "link the TLA+ to the code", "Specula on the Rust", "interleaving search", "the model floats free of the code".
user_invocable: true
---

# port-link

Two gaps this phase closes. First: a TLA+ model that is canaried and model-checked but has *no link to the code* establishes nothing about the port — the source projects said so verbatim in their own concurrency plans. Deductive refinement (`abs_L` with commutation obligations) is the ideal and is usually unstatable across the decode boundary. **Trace validation** is the obtainable version: record real executions of the Rust, project them onto the spec's variables, and require the spec's own `Next` to admit every transition. It is refinement *sampled on observed executions* — not a proof, but it bites the exact failure the deductive route hits (a spec drifting from the code), because a trace comes from the shipped binary and cannot be satisfied by a copy of it.

Second: a sequential reference model **cannot express concurrency, so no rung derived from it can score a concurrency defect at all**. The one live defect the source projects shipped — id allocation outside the store lock, dropping records under load — was scored by zero of five rungs. Interleaving search on the Rust is the only instrument in the family for that class, and after a JVM→Rust port (`synchronized` → `Arc<RwLock>`, threads → an async runtime) it is the class most likely to be present.

Outputs: `.port/link.md`, `.port/traces/`, `.port/link-harness/`.

## When to Use

Use when:
- `port-optimize` is green and `.port/model/` has a TLA+ spec.
- The target was classified stateful or protocol-shaped in `port-qualify` / `port-audit`.
- The shipped shim has any shared mutable state or async runtime.

Do NOT use when:
- The target is purely sequential and the shim holds no state across calls — record that the concurrency lane is not applicable and why; skip the interleaving search, still run trace validation if a TLA+ spec exists.

## Inputs

- `rust/` (shipped), `.port/model/` (spec, config, projection), `.port/surface.md`
- Specula installed (its `tla-trace-workflow` / `harness-generation` skills and the trace-debugger MCP tools), or an equivalent TLC trace driver

## Workflow

### 1. Classify the trace strategy

Specula's Category A vs B decides the harness: ms-scale operations (network, disk) → a single NDJSON trace file with a mutex-guarded writer and monotonic timestamps; ns-scale (CAS, atomics, spin) → per-thread files with `rdtsc` interval timeboxes, because a mutex on the hot path *suppresses the race you are hunting*. Record the classification; it was decided at `port-qualify` and must not be rediscovered here.

### 2. Instrument the Rust to emit traces

At the points the spec's actions correspond to (the instrumentation map from the model: spec action ↔ Rust symbol), emit the event and the post-state fields the projection needs. Instrumentation is a compile-time feature, never in the shipped default build; the emitted symbol identities are the compiler-derived ones from `port-surface`, and a gate checks the harness *actually invokes* the planned symbols (a harness that names the right function but calls a hand-rolled copy has verified the copy).

### 3. Record traces from three sources

The public corpus replayed through the instrumented build; the generated differential traces; and the concurrency stress legs (many clients, forced contention). Every trace is reproducible from its seed.

### 4. Trace validation against the spec

For each trace: project onto the spec's variables (the same recorded, lossy projection `port-model` trusts), drop steps that leave the projection unchanged, emit the forcing module, run TLC, read TLC's own words — never the exit code (TLC exits nonzero for both the violation you want and a parse error you don't). A step the spec's `Next` does not admit is one of: the Rust is wrong (→ fix, re-run R0/R1 — it *should* have been caught earlier, so also file for `port-alphabet`), the spec is wrong (→ `port-model`, a ledger row, and the fix must move at least one golden or it was unobserved), or the projection is wrong (→ TCB finding). Never edit the projection to make a trace legal.

### 5. Corrupted-trace canary

Take a passing trace, corrupt one transition (swap two events; alter a post-state field), and confirm TLC rejects it. Record the run. A validator that admits the corrupted trace has validated nothing — and because the pass/fail inversion in trace-forcing modules is easy to get wrong, this canary is not optional.

### 6. Interleaving search on the Rust

- `loom` / `shuttle` over the shim's declared action alphabet and context-switch bound — **bounded systematic testing**, labelled as such.
- Specula's Category-B path where the core is lock-free or the shim's races are ns-scale: per-thread timeboxed traces, TLC searching all valid interleavings of overlapping intervals against the spec.
- Every counterexample gets Specula's confirmation bar: reachable through the real interface, a consumer observes the wrong outcome, an executed reproduction with pasted output. `PENDING REPAIR` (the counterexample needs a state the code cannot reach) goes back to the spec, never to the finding list.

### 7. Write `.port/link.md`

Traces validated (count, sources, transitions admitted), canary record, interleaving bounds and results, defects found with reproductions, what the lane cannot see (real preemption, weak memory, OS scheduling), and the refinement claim in its exact form: *"every recorded transition of N traces from these sources is admitted by the spec; a corrupted trace is rejected; this is empirical refinement, not proof."*

## Gate

Every recorded trace admitted; corrupted-trace canary rejected and recorded; interleaving search run at declared bounds with every counterexample either reproduced or repaired upstream. **Canary:** step 5, plus one planted concurrency defect (from the catalogue's scheduling class) that the interleaving search must find.

## Common Mistakes

- **Instrumenting with a mutex on a ns-scale path.** The probe effect serializes the schedule and hides the race. Category B needs timeboxes.
- **Reading TLC's exit code.** Read its output.
- **Editing the projection to make a trace pass.** It is TCB.
- **Calling the result "verified".** Trace validation is sampled refinement; interleaving search is bounded. The ceiling report uses those words.
- **Skipping the lane because the model is sequential.** The model's sequentiality is *why* this lane exists.
