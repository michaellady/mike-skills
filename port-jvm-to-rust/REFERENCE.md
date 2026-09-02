# port-jvm-to-rust — REFERENCE

The model behind the phase family. `SKILL.md` is the procedure; this is why each gate exists, what each claim may say, and the checklists the phases cite.

Sources, so nothing here is re-derived: the assurance ladder, `S_obs`, `DECISIONS.md`, `tlclink` and the kill table come from `twitter-port-matrix`; the Kani real/spec/mutants/negative-control lane, corpus tiering and the polarity defect classes come from `verified-java-websocket-port`; bug archaeology and trace validation come from Specula; per-function spec mining from Syzygy (arXiv 2412.14234); the "executable model + differential random testing" shape from Cedar (AWS VGD); the declarative cross-language corpus from MongoDB's unified test format.

---

## 1. The confidence chain

Equivalence (`∀x. J(x) = R(x)`) is undecidable, so the port is never *proved* equivalent. What is built instead is a chain of links, each with (a) evidence, (b) a demonstrated way to fail, (c) a calibration number, (d) a stated ceiling. **Confidence is the minimum over links, and the ceiling report leads with the minimum.**

Transitivity — `J ⊨ S ∧ R ⊨ S ⟹ J ≡ R` — holds only when `S` *determines the output for every input on the observable surface*. A partial spec (invariants, pre/post) gives "both satisfy S", not "same outputs". So `S` must be a **total, deterministic, executable reference model**, and the invariant spec is a separate, smaller object with a separate role.

| # | Link | Established by | Fails visibly when |
|---|---|---|---|
| 1 | `S` is complete on the observable surface | completeness canary: perturb an output field in `S`; the diff harness must notice | a field is unpinned → equivalence over it was vacuous |
| 2 | `S`'s derivation is auditable | every choice is a `D-number` tracing to observed Java behavior | a decision has no evidence |
| 3 | Nondeterminism is classified | two captures on different JDK builds/hosts, diffed, every divergence resolved | a golden file encodes a sample of a distribution |
| 4 | `J ⊨ S` empirically, with a denominator | replay + differential; branch coverage on the *Java* reported with the unreached list | 100% pass on an alphabet that cannot express the failing input |
| 5 | `Literal-Rust ⊨ S` per method | same harness, same corpus, no abstraction function | any canonicalization not in the ledger |
| 6 | `Optimized ≡ Literal` | Kani per unit, bounded, symbolic inputs; planted-defect controls required to fail | an `nc_*` harness succeeds |
| 7 | Invariants hold on both sides | Kani on optimized Rust; TLC on `S` via the link check, with a corrupted-trace canary | the model floats free of the code |
| 8 | Comparators are in the TCB and treated so | read-only to the porting agent, checksummed, negation-canaried, each rule a `D-number` | the canonicalizer can erase the difference |
| 9 | Calibration from a catalogue not derived from `S` | real defect history + `port-audit` findings + curated operators; kill rate per link | mutants and corpus share a source (selection effect) |
| 10 | Dependency boundary and ceiling are written down | `deps.md`, `CEILING.md` | "1:1 except for eleven libraries" is discovered by the client |

### The two-stage port (where "formal" can live)

`Literal-Rust` mirrors the Java's structure one-to-one — same types as structs, same methods, same control flow, no optimization. `Optimized-Rust` is the real port. This splits the hardest link in two easier ones:

- `Java ≡ Literal` is the *easiest possible* differential problem: isomorphic structure, per-method corpora apply with no abstraction function, the same mutants apply to both sides.
- `Literal ≡ Optimized` is a *same-language* problem, and Kani checks it: `kani::any()` inputs, `assert_eq!(literal_op(x), optimized_op(x))`, plus `abs(optimized_state) == literal_state` across sequences. Bounded, bit-precise, machine-checked — located on the step that introduced the risk.

The claim this licenses: *"proven equivalent (bounded, Kani) to a literal transliteration that agrees with the original on N traces covering M% of reachable branches."*

---

## 2. Claims — the fixed vocabulary

| Say | Only for |
|---|---|
| **formally verified** | the individually named Kani-proved properties (with bounds stated) and the reference-model refinement check |
| **calibrated differential assurance** | the equivalence lane, always with the kill table and the coverage denominator attached |
| **behavior-preserving on the reachable surface** | the port claim; "reachable" is the measured number from `port-alphabet` |
| **defect audit** | what Specula and the archaeology produced |
| **bounded** | every concurrency result (loom/shuttle/interleaving search) — never "proved" |

Never: "1:1", "absolutely correct", "formally verified port", "equivalent" without qualification.

---

## 3. Counts — the four questions

Four different ways a verification count is produced without verification happening (all observed in the source repos): shims counted from a package the verifier could not parse; mutants "killed" because drifted anchors injected nothing; obligations VERIFIED over unreachable code; a contract discharged about a dead branch; "23 verified" of which 11 carried no postcondition and 11 were conditional on hand-written twins, one false of shipped code.

Before quoting any count, clear all four:

1. **Did the verifier parse the file?**
2. **Can the verifier reach the obligation** — does its negation get refuted / does a planted defect fail?
3. **Can the program reach the branch** it describes on a real path?
4. **Is the contract on the shipped symbol**, or on a hand-written copy of it?

---

## 4. Polarity — checks that cannot fail

Every lane of the source projects produced this defect at least once, several *inside the fix for it*. Assume every new gate has it until shown otherwise. The forms:

- an expectation computed by the implementation under test, so the test agrees with whatever the code does
- existence standing in for identity — a path that resolves, a reference that appears somewhere, a digest of the wrong thing
- a substring standing in for a parse — a keyword matched anywhere in free text
- rejecting unknown fields while not requiring modelled ones, so a deleted field yields a zero value that agrees with everything
- a required argument on one function that a lower-level public function bypasses
- a test asserting only *that* something failed, satisfied by the wrong failure
- a harness whose `assume` empties the input space, so real and mutant both "verify"
- a rung pointed at the unmutated original (the mutant registry must name `impl@<id>`, and the rung must run *that* directory)

**Remedy:** prove the gap by execution before fixing it — corrupt the artifact, plant the defect, run the check, *read the passing exit* — then fix, then read the refusal. Two structural moves that ended recurring loops: replace a proxy guard with a direct test of the property; make a constraint unskippable by construction (an opaque verdict type only the gate can produce).

### Canary forms, by what they detect

| Form | Question | Detects | Blind to |
|---|---|---|---|
| **Injection** | if I break the code, does the gate notice? | a gate with no reach | a vacuous proof (the broken statement is downstream of the infeasible point too) |
| **Negation** | if I assert the opposite, can the tool refute it? | an unreachable obligation — claim and negation both verifying is the unique signature | nothing about the gate's *reach* into the code |
| **Planted-defect control** (Kani form) | same spec, applied to a single-defect copy — required to FAIL | both of the above for BMC harnesses: an `nc_*` success means the spec or the bound cannot see the defect | properties the planted defect happens not to touch — so plant one per clause |
| **Clean-substitution** | run the gate with the clean fixture in the bad-canary slot — must fail for "caught nothing" | a runner that treats "ran to completion" as pass | — |

Deductive verifiers (Verus, Gobra, JBMC): injection **and** negation. Kani: planted-defect controls, one per spec clause, plus expected-success controls where two properties overlap (to show which is load-bearing). Every gate runner: clean-substitution.

---

## 5. JVM → Rust semantic impedance checklist

Each item is a `D-number` in the ledger *before line one of Rust*. These are where bit-exact replication silently fails.

- **Strings: UTF-16 vs UTF-8.** Java `String` is UTF-16 code units and can hold **unpaired surrogates**; Rust `String` cannot represent that state. `length()` counts code units. Decide the representation (`String` if surrogates are provably unreachable, else `Vec<u16>` / a `JavaString` newtype) from the alphabet audit, not from hope.
- **`Math` vs `StrictMath`.** `Math.pow/exp/log/sin/...` are not required to be bit-identical across JVMs. If outputs depend on them (financial, actuarial, scientific), bit-exact means porting `StrictMath`'s fdlibm algorithms, not calling `f64::powf`.
- **Hash iteration order.** `HashMap`/`HashSet` order is unspecified but deterministic per JDK + insertion sequence, and legacy code depends on it constantly. Rust's is randomly seeded per process. Census every site; decide per site (ordered map, fixed hasher, or "unordered — canonicalize in the comparator", recorded).
- **Integer semantics.** Java wraps everywhere; Rust panics on overflow in debug, wraps in release. `>>>` vs `>>`. `char` is 16-bit. `int` division/modulo sign rules match; float→int casts do not (`NaN → 0` in Java; saturating in Rust — same result for NaN, different for out-of-range).
- **`BigDecimal`.** Scale is part of `equals` but not `compareTo`; `MathContext` rounding modes; `toString` vs `toPlainString`. No crate replicates this out of the box.
- **Floating-point formatting.** `Double.toString` uses a specific shortest-repr algorithm with `E` notation thresholds; Rust's `{}` differs in edge cases. Same for `Float`.
- **Exceptions as control flow, and their timing.** The *point* at which a throw happens — what partial state or partial output already escaped — is observable. `Result` conversion must preserve when, not just whether.
- **Null.** Pervasive; `Option` is the port, but `null` as a sentinel in collections, maps returning `null` for missing vs present-null, and `Objects.equals` semantics each need a rule.
- **Identity vs equality.** `==` on objects, `identityHashCode`, `IdentityHashMap`, interned strings. Any output that leaks identity is nondeterministic (see §6).
- **Collections contracts.** `ArrayList` growth is unobservable; `LinkedHashMap` access order, `TreeMap` comparator consistency with `equals`, `ConcurrentHashMap` weakly-consistent iteration are observable.
- **Regex.** `java.util.regex` vs `regex` crate: backreferences, lookaround, possessive quantifiers, Unicode classes, `\b` semantics. Enumerate every pattern; test each against the corpus.
- **Charsets and locale.** Default charset, `String.getBytes()` with no argument, `toUpperCase()` (Turkish `i`), `String.format` with locale-dependent separators, `SimpleDateFormat`.
- **Time.** `System.currentTimeMillis` granularity, `nanoTime` monotonicity, timezone data version, `java.util.Date` mutability.
- **Concurrency model.** `synchronized`/`volatile`/JMM happens-before → `Arc<Mutex/RwLock>`, `Send`/`Sync`, and an async runtime. Anything observable that depended on JMM visibility has no expression in a sequential reference model (see `port-model`); it is hunted, not modeled.
- **Serialization.** `java.io.Serializable` streams, `hashCode`-dependent layouts, `transient`. Usually a boundary decision: reproduce the bytes, or declare the format out of surface.
- **Reflection, classloading, JNI, `Unsafe`, finalizers.** No reference model expresses these. In the core → stop (`port-qualify`). In configuration → boundary decision.

---

## 6. Nondeterminism census

Run the capture twice on **different JDK builds and different hosts**. Diff. Every divergence is one of:

| Class | Example | Resolution |
|---|---|---|
| **environmental** | default charset, locale, timezone, line separator | pin in the surface contract; capture under the pinned values |
| **identity** | `Object.toString` hash, `identityHashCode`, unordered `HashMap` iteration | canonicalize in the comparator (recorded as a `D-number`) or declare out of surface |
| **temporal** | clock reads, timeouts, timer granularity | inject a controllable clock through the adapter; time is an *input* |
| **scheduling** | thread interleaving, executor ordering, GC-dependent finalizer timing | out of the sequential model; covered by the concurrency lane only |
| **library variance** | `Math.*`, `Random` seeding, `UUID.randomUUID` | seed or route through the adapter; if truly variant, the field is out of surface |
| **real defect** | the two runs differ because the Java has a race | `port-audit` finding; ledger ruling |

Nothing becomes a golden file until every divergence has a row.

---

## 7. Artifact schemas

### Ledger row (`DECISIONS.md` / `.port/model/DECISIONS.md`)

```
## D<n> — <one-line title>
**Question.** What the model / spec / RFC left open.
**Observed.** What the pinned original does, with evidence (corpus step, trace id, method + inputs).
**Decision.** What the reference model does. PRESERVE | FIX | OUT-OF-SURFACE | CANONICALIZE.
**Constraint.** The exact rule, executable if possible.
**Consequence.** What downstream artifacts this changes; what would now be a false failure.
**Ruling.** owner | inherited | default — and for security-relevant rows, the owner's explicit ruling with date.
```

### Observable-surface contract (`.port/surface.md`)

For each surface (an endpoint, a public method, a wire format, a file format, a log/metric channel): `id`, `kind` (wire | api | file | telemetry | exit-status), **in-relation** (byte-exact | canonicalized-by D<n> | out-of-surface), the oracle that pins it, and the edge cases enumerated (`nil, empty, malformed, oversized, duplicate, interrupted, stale, upstream-error`).

### Dependency disposition (`.port/deps.md`)

For every non-JDK dependency in the transitive closure: `port` (its behavior enters the corpus), `wrap` (call the same library across a boundary; note the boundary is TCB), `replace` (a Rust equivalent; **characterize the delta** with its own corpus), `out-of-surface`. JDK modules used get a row too (`java.util.regex`, `java.text`, `java.math`).

### Calibration table (`.port/calibration.md`)

```
link                       mutants  live  killed  survived  unreached  kill%  launches  wall
Java  ⊨ S   (replay)
Java  ⊨ S   (diff)
Lit   ⊨ S   (per-method)
Opt   ≡ Lit (kani)
invariants  (kani / tlc)
```
Compare by **launches**, not seconds; subtract the per-launch floor. Lead with the caveat: which rows share a source with the catalogue.

### Finding (`.port/learn/F<nnn>-<slug>.md`)

Fixed shape in `port-learn/SKILL.md` step 1: mechanism-not-symptom title, phase + step + date, what happened (from tool output), what it cost, **where the deciding moment was** (the exact skill step a rule would have fired at), evidence. Binned as REDISCOVERY | NEW RULE | TOOL GAP | TARGET-LOCAL; the first three change this family via a reviewed PR that quotes the sentence that would have prevented the cost.

### Ceiling report (`.port/CEILING.md`)

Headline = the weakest link. Then per link: evidence, canary, calibration, ceiling and its cause (a tool gap is a *result*, not an excuse). Then: coverage denominator and the unreached list; nondeterminism census; dependency dispositions; the preserve/fix rulings; the cutover readiness ladder (`SOURCE_QUALIFIED → SEMANTICALLY_VERIFIED → OPERATIONALLY_VERIFIED → SHADOW_VERIFIED → CANARY_VERIFIED → CUTOVER_READY`); cost per rung; speedup and memory delta; the receipt (every tool digest, model and version).

---

## 8. Tools by lane

| Lane | Java side | Rust side | Cross |
|---|---|---|---|
| Capture / oracle | ByteBuddy or ASM agent (per-method args/returns); AREX for production traffic; JFR | — | per-method JSONL corpora, declarative and schema-validated (MongoDB UTF style) |
| Coverage / alphabet | Jazzer, JQF; JaCoCo branch coverage as the denominator | cargo-fuzz, bolero | one corpus drives both; diff |
| Defect audit | **Specula** (protocol-shaped targets only); git/issue archaeology | — | Scenarios → catalogue |
| Reference model | — | the model itself is Rust (doubles as the value core) or Go | `tlclink`-style refinement check with canary |
| Deductive | skip — JBMC cannot compare strings; OpenJML/KeY do not scale | **Kani** (shipped source via path shim; real/spec/mutants/negative_control) | — |
| Concurrency | jcstress to characterize what the original guarantees | loom, shuttle, Miri, TSan | **Specula** trace validation / Category-B interleaving search |
| Mutation | PIT | cargo-mutants + curated operator table | kill table per link |
| Performance | JMH, JFR, JOL | criterion, iai, dhat-rs | same workload; pass/fail gate |
| Memory | JOL histogram, GC logs | RSS over 24 h, allocation counts, fragmentation under jemalloc/mimalloc | overhead ratio |
| Cross-model | — | — | `converge audit` on every agent-written comparator |

Kani over Verus for the port product: bounded but on the actual shipped code, zero-annotation panic-freedom, no hand-written twins to drift. Verus only for pure value cores where the lock is already lifted.

---

## 9. Layer selection — what `maximize-verification` should promote for a port

Hand `maximize-verification` the unit and these promotions:

- **Tier 0**: differential/conformance (always); **stateful / model-based** (any stateful unit — the oracle is sequence-based); **performance** and **complexity / cost-bound** (the buyer is buying this); **compatibility / migration** (wire and file formats, API surface diff).
- **Tier 1 additions**: **sanitizers** become mandatory the moment `unsafe` appears in the optimized Rust; **resource-leak / liveness** and a 24-hour soak for memory-bound targets (fragmentation is the #1 way a memory port disappoints).
- **Memory-bound targets**: the decomposition unit is the *module with a representation boundary*, not the method; the oracle moves to the module interface and to operation sequences; peak RSS, bytes-per-entry and RSS slope are goldens, not reports.
