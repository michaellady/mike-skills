---
name: port-capture
description: Use when you need the "impression" of a pinned JVM original — per-method and sequence-level golden corpora captured by bytecode instrumentation (no source changes), tiered into public / hidden / sealed, captured twice on different JDK builds and hosts so every nondeterministic field is classified before any golden file is trusted. Phase 2 of port-jvm-to-rust. Triggers — "capture the Java behavior", "characterization corpus", "golden corpus from the jar", "pin existing behavior", "record what the Java does", "per-method oracle", "nondeterminism census".
user_invocable: true
---

# port-capture

Build the oracle. The pinned original is the strongest oracle a port can have — it is the *original*, not a sibling — but it is only an oracle at the granularity you capture it. Capturing at the public API alone gives one enormous diff with no localization and a reach bounded by the top-level alphabet. Capturing **per method** by bytecode instrumentation turns one unverifiable port into hundreds of independently verifiable units (the Syzygy decomposition), and the JVM makes this nearly free: a Java agent records every method's arguments and returns with zero source changes.

Then the part everyone skips: **the Java is not a function.** Hash order, identity hashes, clock reads, default charset, `Math.*` variance, scheduling. One capture bakes a *sample* of each into golden files as if it were contract. Capture twice, on different JDK builds and hosts, diff, and classify every divergence — before a single golden file is trusted.

Outputs: `.port/corpora/{public,hidden,sealed}/` (declarative, schema-validated, one runner per language) and `.port/nondeterminism.md`.

## When to Use

Use when:
- `.port/surface.md` is signed and no corpus exists.
- An existing corpus was hand-written or derived from a spec rather than captured from the running original (it needs replacing, not extending — see `port-model` on why spec-derived expectations are self-fulfilling).
- A corpus passes 100% and you suspect it cannot fail.

Do NOT use when:
- There is no runnable pinned original.
- The surface contract does not exist — you would be capturing without knowing what to compare.

## Inputs

- `.port/surface.md`, `.port/deps.md` (which channels are in-relation; which deps are `replace` and need their own delta corpus)
- The pinned jar by digest, verified at adapter start-up (never a classes directory, never an unpinned build)
- Optionally: production traffic (an AREX-style Java agent recording real requests and mocking third-party calls) and the original's own test suite

## Workflow

### 1. Build the JSONL adapter over the pinned jar

A dependency-free process that loads the jar by digest, verifies the digest before the loop starts, and drives the surface through a line-oriented protocol: request in, observation out, canonical output bytes. Time is an **input** (inject a controllable clock); the adapter never writes to implementation state — replay drives the system only through the observable surface (the self-fulfilling-clock defect: a harness that sets the clock to the expected answer before asserting on it passes for every possible clock rule).

### 2. Attach the per-method recorder

A Java agent (ByteBuddy or ASM) weaving entry/exit advice into every method on the study surface: arguments, return value or thrown exception, and a sequence number, serialized through the same canonical encoder the adapter uses. Filter to the surface's packages; record `this`-state only where the surface contract makes it observable. Run it under the pinned environment from `.port/surface.md`.

For **memory-bound targets whose representation will change**, also record at the *module* boundary (the interface of each data structure) and as operation *sequences* with a canonical state dump at the end — the per-method corpus will not survive a representation change, the module-boundary sequence corpus will.

### 3. Drive it from three sources, kept separate

| source | how | what it buys |
|---|---|---|
| the original's test suite | run `mvn test` / `gradle test` under the agent | free, immediate, happy-path biased |
| generated traces | a seeded generator over the surface's request alphabet, including every malformed shape the contract enumerates | coverage of edge-case families; shrinkable failures |
| production traffic | AREX-style capture with third-party mocks | the only source whose *distribution* is real |

Record which source produced each scenario. `port-alphabet` measures what each reaches.

### 4. Capture twice, then census

Run the whole capture a second time on a **different JDK build and a different host** (different OS if possible). Diff every recorded field. Every divergence gets a row in `.port/nondeterminism.md` with one class and one resolution — see `port-jvm-to-rust/REFERENCE.md` § 6: environmental (pin), identity (canonicalize by `D-number` or out-of-surface), temporal (inject), scheduling (out of the sequential model; concurrency lane), library variance (seed/route), or **real defect** (hand to `port-audit`).

Also run the capture twice on the *same* host: identical scenario bytes producing different observations is a finding (`NONDETERMINISTIC_OBSERVATION`), never a flake to rerun.

Nothing is promoted to a golden file until its every field is either byte-stable across both environments or has a census row.

### 5. Tier and seal

- **public** — what the porting agent sees.
- **hidden** — held out until the agent declares done; run once per declared-done, never disclosed.
- **sealed** — held out until the ceiling report; the generalization check on the hidden tier itself.
Manifest each tier with a digest. The porting agent is read-only on all three, and never sees hidden or sealed contents.

### 6. Write the canonicalizer, and canary it

Every `canonicalized-by D<n>` rule from the surface contract becomes code in the comparator. The comparator is TCB: read-only to the porting agent, reviewed by a different model (`converge audit`), and **negation-canaried** — inject a known difference in a canonicalized field; the comparator must still report it unless the ledger row says otherwise. A canonicalizer that can erase every difference has verified nothing.

### 7. Prove the corpus can fail

Run the whole corpus against an empty / stub implementation: it must fail every scenario in every tier, non-vacuously (read the failure lines, not the exit code). Then run it twice against the original: byte-identical transcripts, reconciled by digest.

## Gate

Two captures diffed and every divergence classified; empty stub fails every scenario in every tier; canonicalizer negation canary recorded; tiers manifested and sealed. **Canary:** plant one behavioral change in a scratch copy of the original (a single guard flipped); the public tier must catch it. If it does not, the corpus does not reach that guard — file it for `port-alphabet`.

## Common Mistakes

- **One capture.** The nondeterminism census is the step that makes "same outputs" well-defined. Skipping it produces false R0 failures forever, and worse, golden files that are wrong and stable.
- **Capturing through a spec.** Expectations derived from a model of the system are self-fulfilling. Expectations come from the running original.
- **The agent regenerating goldens.** "Never re-baseline to make a difference disappear." A shift is a finding.
- **Recording internal state that is not on the surface.** It leaks representation into the oracle and breaks the moment `port-optimize` changes it.
- **Trusting the exit code of the empty-stub run.** Read the per-scenario failures; a runner that returns 0 after zero assertions has passed nothing.
