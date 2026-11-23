---
name: port-surface
description: Use when a JVM-to-Rust port needs, before any capture or porting, an exact definition of what "same behavior" means for a JVM-to-Rust port — the observable-surface contract (what is inside the equivalence relation, what is canonicalized, what is out) and the dependency boundary map (for every library the original pulls in, whether it is ported, wrapped, replaced or excluded). Phase 1 of port-jvm-to-rust. Triggers — "what counts as output", "define the equivalence relation", "observable surface", "where does the port stop", "dependency boundary", "which behaviors are in scope for the port".
user_invocable: true
---

# port-surface

"Same inputs, same outputs" is a slogan until two documents exist: **what counts as an output**, and **where the port stops**. Logs, metrics, exception messages, stack traces, JSON key order, trailing whitespace — downstream systems parse all of these. And the behavior you capture is jar + transitive dependency closure + JDK; every third-party boundary is a divergence surface you don't own. Everything left unstated here is unverified *by accident*; everything stated as out-of-surface is unverified *by agreement*, which is fine.

Outputs: `.port/surface.md` (the contract, signed by the owner) and `.port/deps.md` (the boundary map). Every later phase is defined relative to these.

## When to Use

Use when:
- `port-qualify` returned GO and no surface contract exists yet.
- Anyone says "1:1", "drop-in" or "identical" about the port — this is where that becomes a proposition.
- A differential harness is about to be written and nobody has said which fields it compares.

Do NOT use when:
- The port is a redesign where behavior is allowed to change (then write acceptance criteria, not a surface contract).

## Inputs

- `.port/qualify.md`
- The pinned original, its public API, wire/file formats it reads or writes, and its telemetry channels.
- The owner (the person who can sign the contract).

## Workflow

### 1. Enumerate every channel the original can be observed through

Walk, do not guess. For each: `kind` ∈ { wire, api, file, telemetry, exit-status, side-effect }.

- **wire**: every protocol the process speaks; every byte it emits, including framing, ordering, and error envelopes
- **api**: every public method reachable by a consumer (from the compiler's symbol table — javac attribution, not grep), with checked and unchecked exceptions as part of the signature
- **file**: everything read or written on disk, including formats, encodings, line endings
- **telemetry**: log lines, metrics, spans, exit codes — and *who consumes them* (an ops team grepping for exact error text makes the text part of the surface)
- **side-effect**: environment mutations, spawned processes, network calls out

### 2. Assign each surface an in-relation

| in-relation | meaning |
|---|---|
| `byte-exact` | compared byte-for-byte; the default for wire and file |
| `canonicalized-by D<n>` | compared after a *recorded* normalization (unordered map iteration, timestamps, generated ids); every rule is a ledger row and the canonicalizer gets its own negation canary in `port-capture` |
| `out-of-surface` | not compared; the owner agrees this is unverified — with the reason |

For each surface also enumerate the edge-case families the corpus must reach: `nil, empty, malformed, oversized, duplicate, interrupted, stale, upstream-error`. `port-alphabet` reports which of these the corpus actually hits.

### 3. Pin the environment

The surface is only defined under a pinned environment: JDK build, default charset, locale, timezone, line separator, `file.encoding`, and the flags the original runs with in production. Record them here; `port-capture` captures under them and *also* under a second environment to census nondeterminism.

### 4. Build the dependency boundary map

From the build tool's dependency tree (`mvn dependency:tree` / `gradle dependencies`), every non-JDK artifact in the *runtime* closure, plus the JDK modules whose behavior is observable (`java.util.regex`, `java.text`, `java.math`, `java.time`, `java.util.zip`). For each, one disposition:

| disposition | consequence |
|---|---|
| `port` | its behavior enters the corpus and the port; it is now *your* code |
| `wrap` | call the same library across a boundary (JNI-in-reverse, a sidecar); the boundary is TCB and its cost is measured |
| `replace` | bind to a Rust equivalent (`serde` for Jackson, `regex` for `java.util.regex`); **the delta is characterized with its own corpus** in `port-capture` and every divergence is a ledger row |
| `out-of-surface` | excluded; the owner signs |

`replace` is where "1:1" quietly becomes "1:1 except for eleven libraries". The map makes that sentence explicit before the client discovers it.

### 5. Write the two files and get the signature

`.port/surface.md`: the table from steps 1–2, the environment pin from 3, and the owner's sign-off line with date. `.port/deps.md`: the table from step 4. The ledger (`.port/model/DECISIONS.md`) does not exist yet; reserve `D-numbers` here for every `canonicalized-by` rule so `port-model` fills them in.

## Gate

The owner has signed `.port/surface.md`, and every runtime dependency has a disposition. **Canary:** pick one channel marked `out-of-surface` and one marked `byte-exact`; confirm the eventual harness *ignores* the first and *fails* on a one-byte change to the second. A contract the harness does not implement is prose.

## Common Mistakes

- **Enumerating from the README instead of the symbol table.** Public surface comes from the compiler's view (javac `javax.lang.model`, or the LSP), not from documentation.
- **Forgetting telemetry.** The error text an ops runbook greps for is an output.
- **Leaving `replace` deltas uncharacterized.** `serde` and Jackson disagree on plenty; each disagreement is either a ledger row or a live bug.
- **Signing the contract yourself.** The owner signs. The port agent is the party with the incentive to narrow it.
- **Treating exceptions as not-output.** Which exception, its message, and *when* it is thrown (what partial output escaped first) are all observable.
