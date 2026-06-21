# maximize-verification — REFERENCE

The full testing landscape behind the [SKILL.md](./SKILL.md) procedure. SKILL.md builds the maximal verification stack by anchoring on the strongest oracle and then layering every independent check the code admits; this file is the catalog of layers and tools it draws from. Two axes matter: **level** (what scope) and **strategy** (how you decide what's correct). Strategy is the more interesting axis — each strategy is an independent failure-mode lens, and stacking lenses with *uncorrelated* failure modes is what maximizes verification. This is where the reliability of agent work is won or lost.

---

## Testing by level

The familiar pyramid — but the boundaries matter less than people think.

- **Unit tests** — a single function/class in isolation. Fast, deterministic, cheap to run thousands of.
- **Integration tests** — real collaborators wired together (DB, queue, filesystem). Catches the wiring bugs units can't.
- **End-to-end / system tests** — the whole app through its real interface. Slow and flaky-prone, but the only thing that tests what the user actually experiences.
- **Acceptance / BDD tests** — verify user-facing behavior against business-readable acceptance criteria, written as executable scenarios (specification-by-example). The spec doubles as a human-reviewable contract — which is exactly the "tests are the spec" lever. Tools: Cucumber/Gherkin, behave (Py), SpecFlow (.NET).
- **Contract tests** — verify the interface between two services independently (Pact-style consumer-driven contracts). Lets you test an integration boundary without standing up both sides.
- **Characterization / pinning tests** — Feathers' term: tests that capture *existing* behavior of legacy code before you touch it, even if that behavior is "wrong." The required first move for any refactor/port of code you don't fully understand.

---

## Testing by strategy

Levels tell you *what scope*; strategy tells you *how you decide what's correct*. This is where agent reliability lives.

- **Example-based** — the default: hand-pick inputs, assert outputs. Limited by your imagination — the core weakness for agent work, which is good at the happy path.
- **Doctest / example-correctness testing** — execute the examples embedded in docstrings, READMEs, and API docs and assert they still produce the stated output. The documentation form of example-based testing: the example doubles as a tiny human-readable spec, and running it keeps the docs from rotting — a frequent failure in generated code, where plausible-but-wrong examples are common. Tools: Python `doctest`, Rust doc-tests (`cargo test`), Go testable `Example` functions, nbval/testbook (notebooks), tsd (TS type-level examples).
- **Property-based** — assert invariants over machine-generated inputs. "Reversing twice is identity," "output is always sorted," "encode/decode round-trips." The generator finds edge cases you'd never enumerate. Tools: Hypothesis (Py), fast-check (JS/TS), proptest/quickcheck (Rust), jqwik (Java), gopter/rapid (Go).
- **Stateful / model-based testing** — model the unit as a state machine; the tool generates *sequences* of operations and checks invariants/postconditions after each step. Catches lifecycle and ordering bugs that one-shot property tests can't see. Tools: Hypothesis `RuleBasedStateMachine`, proptest-state-machine (Rust), fast-check model-based commands, jqwik stateful, rapidcheck (C++). Essential for caches, queues, DBs, allocators.
- **Metamorphic testing** — for when you have *no oracle*: test relations between outputs instead of absolute values. If `sort([3,1,2])` and `sort([2,1,3])` must be equal, you can test sorting without knowing the answer. Underused and powerful.
- **Statistical / distributional testing** — when the output is a *random variable* (ML models, samplers, randomized algorithms, LLM features), correctness is a property of the output *distribution*, not a fixed value. Assert mean/variance/quantiles, calibration, or a KS/χ² fit against a reference distribution, and use significance tests to compare variants. Distinct from metamorphic (relations) and property-based (invariants). **Two non-negotiables:** (1) a declared protocol — statistic, sample size/power, alpha, effect-size threshold, multiple-comparison handling, deterministic seed; (2) the reference must come from a source the generating agent didn't produce (trusted old impl, curated/held-out data, reviewed prod telemetry) — a self-generated golden is weak monitoring, not a blocking gate — with a versioned re-baselining rotation so legitimate drift updates it. Tools: scipy.stats, Hypothesis statistical helpers, deepchecks, evidently/NannyML (drift).
- **ML/LLM behavioral evaluation** — for models and LLM features, *extend* statistical/distributional testing with three lenses it doesn't cover. **Adversarial robustness:** does a small, crafted perturbation flip the output (FGSM/PGD for vision/NLP models; prompt-injection for LLMs)? **Fairness / bias:** do outcomes differ across protected groups beyond a declared threshold? **For LLMs specifically:** a curated *eval set* with graded rubrics, **LLM-as-judge** scoring (with a human-reviewed rubric — the judge is itself an agent, so beware correlated failure), and **red-team / jailbreak** suites for unsafe outputs. Relationship to neighbors: the distribution checks are the statistical row; the adversarial/jailbreak suites share the *security* mindset (abuse-case tests applied to a model). Tools: deepchecks, Fairlearn/AIF360 (bias), ART / TextAttack (adversarial), promptfoo / OpenAI evals / Inspect (LLM eval), garak (LLM red-team).
- **Observability / telemetry-emission assertions** — assert that the code *emits* the expected telemetry on key paths: the error path increments the right metric, a span/attribute is recorded, a critical event fires with approved dimensions. Catches *silent monitoring failure* — code that's functionally correct but whose production failures would be invisible. Test **emission** via in-memory exporters / structured-log capture with sampling off; assert event presence + approved dimensions/cardinality, **not** full log snapshots or vendor formatting (those rot), and note that alert/SLO *firing* is monitoring config, not a code test. Tools: OpenTelemetry test exporters.
- **Differential / conformance testing** — run two implementations on the same inputs and diff. **This is the "golden artifacts" idea: the old implementation is the oracle for the new one.** The safest spine for refactor/optimize/port work.
- **Shadow / production differential** — the production-time form of differential testing: mirror *real* traffic to the old and new implementations in parallel and diff their outputs before promoting the new one. The canonical way to de-risk a rewrite at scale. Tools/patterns: GitHub Scientist, Twitter Diffy, dark launch, canary.
- **N-version / redundancy** — run two or more *independently produced* implementations on the same inputs and diff or majority-vote. Generalizes differential testing to the case where no single impl is trusted; cross-model adversarial review is its LLM-authored form. The independence is the whole point — shared bugs defeat it.
- **Snapshot / golden / approval testing** — freeze a known-good output artifact, fail on any diff. Cheap to create, but rots if nobody reviews the diffs. (The UI-shaped form is *visual regression*, below.)
- **Visual regression** — snapshot the *rendered* output (a page, component, chart) and fail on pixel/layout diffs that DOM- and logic-level assertions never see. Tools: Percy, Chromatic, Playwright/Storybook screenshots. The UI-shaped form of snapshot testing.
- **Accessibility & localization testing** — two lenses pixel diffs structurally miss. **Accessibility (a11y):** assert semantic/operability rules — color-contrast ratios, ARIA roles, alt text, keyboard navigation, focus order — that a screenshot can't see. Tools: axe-core, pa11y, Lighthouse CI. **Internationalization / localization (i18n/l10n):** catch hardcoded strings, broken right-to-left layout, character-encoding/Unicode handling, and text that truncates or overflows once translated. Technique: *pseudolocalization* (accent + expand every string) and RTL snapshots surface most of these before real translations exist. For user-facing UI only.
- **Mutation testing** — deliberately inject bugs and check whether the tests catch them. Measures *test quality* (catch-power), not code coverage. Tools: Stryker (JS/TS), mutmut/cosmic-ray (Py), PIT (Java), cargo-mutants (Rust), go-mutesting (Go).
- **Flakiness / nondeterminism detection** — the other suite-quality meta-check (sibling to mutation testing): mutation measures *catch-power*; this measures *verdict stability*. A suite that passes/fails nondeterministically is coin-flip green and voids every other layer. Rerun under randomized order/seed/clock/parallelism to *expose* instability — and treat it as detection, not a pass mechanism: **default flake-budget = 0** for a blocking gate, with any quarantined exception kept as a *separate failing* stability signal until fixed (never silently dropped). Tools: pytest-randomly + reruns, `go test -count/-shuffle`, Maven surefire rerun, test-retry quarantines.
- **Time / clock / timezone testing** — exercise the time-dependent logic that ordinary tests run only at "now" in one timezone: DST transitions, leap seconds/years, timezone and locale boundaries, token/cache *expiry*, and ordering of timestamped events. The enabling move is a **controllable clock** — inject a `Clock` interface (never call `time.Now()` directly) so tests can freeze, advance, and warp it. This is also a flakiness *fix*, not just a detector: wall-clock dependence is a top source of nondeterministic tests. Tools: libfaketime, freezegun (Py), `clockwork`/injectable clocks (Go), jest/sinon fake timers (JS).
- **Fuzzing** — coverage-guided input generation that finds crashes and panics in the input space. Tools: libFuzzer, AFL++, Go native fuzzing (`go test -fuzz`), cargo-fuzz, Atheris (Py). Multiplies in value when run under sanitizers and with runtime contracts enabled.
- **Sanitizers / dynamic analysis** — instrument a running program to catch memory-safety and undefined-behavior bugs that produce correct-looking output until they don't: AddressSanitizer (use-after-free, OOB), UndefinedBehaviorSanitizer, MemorySanitizer (uninit reads), LeakSanitizer, Valgrind, and Miri (Rust UB in `unsafe`). Distinct from race detection; critical for native/`unsafe`/cgo code.
- **Resource-leak / liveness testing** — assert that a unit *releases* what it acquires: goroutines/threads, file descriptors, sockets/connections, DB-pool handles, timers, temp files. These pass every functional test — output is correct — yet accumulate until a long-running process exhausts a limit and falls over in production. Distinct from the memory-leak coverage of LeakSanitizer above: this is *liveness/handle* leakage at the runtime level, not heap bytes. Technique: snapshot the resource count before/after an operation (or a loop of operations) and assert it returns to baseline. Tools: go.uber.org/goleak and `runtime.NumGoroutine` deltas (Go), `lsof`/FD counters, JVM thread/handle leak detectors, connection-pool gauges.
- **Symbolic / concolic execution & bounded model checking** — instead of guessing inputs, *solve* path constraints to reach a target path or trip an assertion — or prove no violation exists within a bound. Verifies the actual implementation. Tools: KLEE, SAGE, CrossHair (Py contracts), Kani (Rust BMC), CBMC/JBMC. The exhaustive-within-bounds complement to random fuzzing.
- **Concurrency testing** — race detectors (`-race`, ThreadSanitizer), exhaustive interleaving explorers (Loom for Rust, jcstress for Java), and deterministic simulation testing (FoundationDB-style: Antithesis, madsim, turmoil). Essential for transaction-heavy, high-concurrency targets.
- **Distributed-consistency / linearizability testing** — for replicated stores, consensus, and multi-node transactions, the bug class is *consistency violation under concurrency + partition* — invisible to single-node concurrency tests. Generate concurrent client histories while injecting partitions/clock-skew, then check the recorded history against a consistency model (linearizability, serializability, causal). Tools: Jepsen with the Elle / Knossos checkers, Stateright, Maelstrom; or refinement-check the design against a TLA+/Alloy model. The next layer out from the concurrency row: that one tests interleavings *within* a process, this one tests guarantees *across* nodes.
- **Performance testing** — benchmarking, load, stress, soak, profiling. Part of "verify," not separate: an isomorphic-but-slower rewrite is a failed optimization.
- **Complexity / cost-bound testing** — distinct from the wall-clock performance row: assert *scaling and resource budgets* rather than latency at one input size. Catch accidentally-quadratic algorithms (does runtime/op-count stay within the expected Big-O as n grows?), **N+1 query** explosions (assert ≤ k database/RPC calls per operation), and allocation budgets. The classic "correct but ruinous" bug — fine at n=100, melts at n=10⁶, invisible to a single-size benchmark. Tools: `testing.AllocsPerRun` + `-benchmem` (Go), `assertNumQueries`/nplusone (Django), query-count interceptors, empirical-Big-O scaling harnesses.
- **Static verification** — type systems, exhaustiveness checks, linters and nullability analyzers (e.g., Error Prone/NullAway for Java), abstract interpretation. Two tiers worth distinguishing: **model checking** verifies an *abstraction* of the system (TLA+, Alloy, Stateright), while **deductive verification** proves the *actual code* against annotations (Dafny, Verus, Gobra, F*, Frama-C). Verifies *without executing* — the cheapest verification there is.
- **Code-quality / maintainability gates** — static analysis aimed at *quality* rather than correctness (the lenses the static-verification row's type/lint/proof checks don't cover): cyclomatic and cognitive complexity ceilings, code duplication, dead-code / unused-export detection, and maintainability metrics, enforced as a CI gate. Agents systematically over-produce needlessly complex and duplicated code; a complexity ceiling is cheap, uncheatable pressure toward code that stays verifiable. Tools: SonarQube, gocyclo/radon/lizard (complexity), jscpd/PMD-CPD (duplication), deadcode/vulture/ts-prune (dead code).
- **Architecture conformance / fitness-function testing** — assert *structural* rules the complexity/duplication gates above can't see: no dependency cycles, layer/module boundaries respected, allowed-import rules, package/visibility isolation. Encoded as executable tests so an agent can't quietly introduce a cycle or a layering violation that compiles and passes every unit test. Agents routinely reach across boundaries for the locally-convenient call; this is the cheap, uncheatable guard for architectural integrity. Tools: ArchUnit (Java/Kotlin), import-linter (Py), dependency-cruiser / eslint-plugin-boundaries (JS/TS), go-arch-lint, deptrac (PHP).
- **Design-by-contract / runtime assertions** — embed preconditions, postconditions, and invariants in the code so every execution — production traffic and every fuzz/property case alike — becomes a check. Tools: `assert`/`debug_assert!`, icontract (Py), Eiffel, Ada/SPARK, Clojure spec.
- **Contract testing** — verify a service/API boundary from each side independently (Pact-style consumer-driven contracts) so integration is checked without standing up both ends. (Also listed under *levels* — it's both a scope and a strategy.) **Data-contract / data-quality testing** is the data-at-rest analogue: assert schema / not-null / range / uniqueness / FK conformance on datasets (Great Expectations, dbt tests, Deequ, Pandera, Soda). Keep it *hermetic* in CI (synthetic or local fixtures) — once hermetic it largely overlaps contract + property testing; the genuinely distinct part, *live* drift / freshness / volume on production data, belongs in operational monitoring, not a blocking code-change gate.
- **Compatibility / migration testing** — for ports and persisted state: round-trip old↔new serialized data and wire formats, and test schema migrations forward *and* backward (upgrade/downgrade), so a rewrite doesn't silently lose or corrupt data. Includes **API/ABI surface diffing** — golden the public exported signatures and fail on unintended semver-breaking changes (cargo-semver-checks, go-apidiff, api-extractor, japicmp).
- **Combinatorial / pairwise testing** — for large configuration/feature-flag spaces, generate an all-pairs (or n-wise) covering set to catch interaction bugs without the full cross-product. Tools: PICT, ACTS, allpairspy.
- **Chaos / fault injection** — kill processes, inject latency and errors, verify graceful degradation.
- **Crash-recovery / durability / crash-consistency testing** — the persistence-correctness sibling of chaos: where chaos asks "does it *degrade* gracefully," this asks "does *committed state survive a crash and recover intact*." Enumerate crash points (including mid-write and mid-`fsync`), kill the process hard, restart, and assert no corruption, no lost acknowledged writes, and correct recovery — exercising WAL/journal replay, torn-write handling, and fsync ordering. Essential for storage engines, databases, file formats, and any stateful service. Tools: CrashMonkey/ACE, ALICE, dm-flakey / device-mapper fault injection, failpoint-driven crash injection.
- **Negative / error-path & failure-atomicity testing** — the non-security sibling of the abuse-case tests below: deliberately drive failures and assert the code fails *correctly*. Two things happy-path suites skip: the **error surface** (the right error type/message is raised — not a generic crash, and not a swallowed exception) and **failure atomicity** (an operation that fails partway leaves no partial state — it rolls back or cleans up). Inject failures at boundaries (a DB drop mid-transaction, a write that hits ENOSPC) and assert the post-failure invariant. Agents reliably test the happy path and leave partial-write / half-rolled-back bugs. Tools: failpoint injection (gofail, toxiproxy), transaction-rollback assertions, `errors.Is/As` (Go) / typed-exception assertions.
- **Security testing** (adjacent axis) — SAST and taint analysis, DAST, dependency/SCA and secret scanning, and fuzzing-for-vulnerabilities, plus **developer-authored abuse-case / negative tests** (authz & tenant-isolation bypass, input-validation boundaries, business-logic bypass, secret non-leakage). The abuse-case tests are mechanically negative example/property tests applied with an adversarial mindset — a *lens on intent*, not a structurally new mechanism. Outside pure correctness, but part of "maximally verify" for anything exposed.

---

## Expressing coverage — and the trap

Coverage metrics, roughly weakest to strongest:

- **Function/method coverage** — was each function called at all.
- **Line / statement coverage** — was each line executed.
- **Branch / decision coverage** — was each branch taken both ways.
- **Condition coverage & MC/DC** — every boolean sub-condition independently affects the outcome. MC/DC is the avionics standard (DO-178C) and genuinely rigorous.
- **Path coverage** — every combination of branches. Combinatorially explodes; mostly theoretical.

**The trap:** coverage measures what was *executed*, not what was *verified*. A test with zero assertions that merely calls the code gives 100% line coverage. This is why **mutation score** is the metric that correlates with reliability — it tells you whether your tests would *catch* a bug, not just whether they walked past the code. For agent-generated suites, report the mutation score, not line coverage.

---

## Maximizing reliability of agent-generated code

The core problem with an agent writing both the code *and* its tests is **correlated failure**: if the agent misunderstands the spec, it misunderstands it identically in both, and the green checkmark is meaningless. Reliability comes from breaking that correlation.

The strongest lever: **use an oracle the agent didn't produce.** For optimization and refactoring the previous implementation is a free, perfect oracle — a differential/conformance harness over golden artifacts means the agent literally cannot "agree with itself" into a bug. This is why domain ignorance is survivable when correctness is defined as *equivalence to a trusted reference*. It does **not** hold for greenfield code, where no reference exists — there the oracle problem is unsolved and human spec review is unavoidable.

Beyond that, in rough priority order:

1. **Make tests the spec, and protect them.** Tests-first, with a human reviewing the test suite (not every impl line). Make test files read-only to the implementing agent, or split test-writing and code-writing into separate agents/contexts. Otherwise the agent reward-hacks: weakens assertions, adds `skip`, special-cases inputs, hardcodes expected values. Always diff the test files in review.
2. **Hold out tests the agent never sees.** A private suite that runs only after the agent declares done — the generalization check against overfitting to the visible suite.
3. **Lean on property-based, stateful, and metamorphic tests.** Agents are systematically bad at edge cases and ordering and good at the happy path. Generators — especially state-machine generators — don't share that blind spot.
4. **Cross-model adversarial review.** A *different* model as validator has *different* failure modes, so it catches what the implementer's blind spot missed. Same-model review mostly rationalizes.
5. **Shrink the blast radius.** Small diffs, pure functions, narrow typed interfaces. An isomorphic 30-line change is verifiable; a 600-line one is hope.
6. **Push correctness into the compiler.** Strong types, exhaustiveness, linters with `-Werror`. Type errors are the highest-signal feedback you can give an agent — fast, precise, uncheatable.
7. **Determinism.** Seed everything; deterministic simulation for concurrency. A non-reproducible failure is one an agent can't debug, and one you can't trust the fix for.

### The loop: tap condition vs deadlock

A conformance harness *is* the **tap condition** — the loop's clean termination signal: *golden artifacts match **and** benchmarks improved*. It needs an explicit **deadlock detector** (N iterations of no progress → escalate to human) so agents don't grind forever or "win" by quietly mutating the harness. A loop without a legible tap condition doesn't converge — it either thrashes or cheats.

---

## Language → tools quick table

| Language | Property-based | Fuzzing | Mutation | Concurrency |
|---|---|---|---|---|
| **Go** | gopter, pgregory.net/rapid | `go test -fuzz` (native) | go-mutesting, gremlins | `go test -race`, `testing/synctest`, goleak (leaks) |
| **Rust** | proptest, quickcheck | cargo-fuzz (libFuzzer), afl.rs | cargo-mutants | Loom, madsim, ThreadSanitizer |
| **Python** | Hypothesis | Atheris | mutmut, cosmic-ray | — (GIL ≠ race-free; TSan for C-ext) |
| **JS/TS** | fast-check | jsfuzz | Stryker | — |
| **Java** | jqwik | Jazzer | PIT | jcstress, Lincheck |

### Analysis & specialized tools by language

| Language | Stateful / model-based | Sanitizers & UB | Symbolic / BMC | Deductive proof |
|---|---|---|---|---|
| **Go** | rapid (state machines) | `-asan`, `-msan` (cgo/native) | — | Gobra |
| **Rust** | proptest-state-machine | Miri, `-Zsanitizer=address/memory` | Kani (wraps CBMC) | Verus, Prusti, Creusot |
| **Python** | Hypothesis `RuleBasedStateMachine` | (via C-ext ASan) | CrossHair | Nagini |
| **JS/TS** | fast-check (model-based) | — | — | — |
| **Java** | jqwik (stateful) | — | JBMC | KeY, OpenJML |
| **C/C++** | rapidcheck | ASan/UBSan/MSan/LSan, Valgrind | CBMC, KLEE | Frama-C |

Cross-language differential/conformance and golden-artifact testing are framework-agnostic — capture the old implementation's outputs over a corpus, run the new implementation on the same inputs, and diff. Deterministic simulation testing (FoundationDB-style) is available via Antithesis (hosted), madsim (Rust), and turmoil (Rust networking). Shadow / production differential is likewise framework-agnostic — GitHub Scientist (Ruby + ports), Twitter Diffy (HTTP services), or a hand-rolled mirror that tees real traffic to old + new and diffs.

---

## Worked example: optimizing a Go hot path

A concrete fill-in of the [SKILL.md output format](./SKILL.md#output-format) — every slot named, the tiered stack marked on/off/why.

*An agent is optimizing a hot path in a Go service — `func Route(req Request) []Hop` — replacing an O(n²) scan with a precomputed index. The old implementation stays in the tree. This is the highest-value case: a free, perfect oracle exists.*

1. **Diagnosis** — Change type: refactor/optimize with existing impl. Oracle: the old `Route` (kept). Authorship: the same agent writes the new impl *and* its tests → correlated-failure flag **set**. Nature: performance-critical, single-threaded, trusted input. Current verification: a handful of example tests.
2. **Anchor oracle** — **Differential / conformance** over golden artifacts: capture `oldRoute`'s output across a request corpus, run `newRoute` on the same inputs, diff. The agent cannot "agree with itself" into a bug.
3. **The stack** —
   - *Tier 0 — Differential:* **ON** — `oldRoute` vs `newRoute` over the corpus + property-generated inputs. The spine.
   - *Tier 1 — Static:* **ON** — `go vet` + golangci-lint as a `-Werror` gate. *Property-based:* **ON** — gopter/rapid: `newRoute(req)` ≡ `oldRoute(req)` for all generated `req` (differential-as-property). *Runtime contracts:* **ON** — `assert` output hops form a valid path.
   - *Tier 2 — N-version:* **ON via differential** (old impl is the second version). *Cross-model review:* **ON** — agent authored code+tests (see step 7).
   - *Tier 3 — Performance:* **ON** — `testing.B` benchmark; the perf delta is a pass/fail gate (a slower rewrite is a *failed* optimization). *Fuzzing:* **ON** — `go test -fuzz` over `Request`, differential oracle as the check. *Compatibility:* **ON** if `Request`/`Hop` cross a wire/persisted boundary; else off. **OFF (why):** concurrency (single-threaded), sanitizers (pure Go, no cgo/unsafe), statistical (deterministic output), visual/BDD/chaos/observability/shadow (not user-facing, not distributed), symbolic (differential+fuzz already cover the logic), stateful/metamorphic/combinatorial/contract (no state machine, no config matrix, no unowned boundary).
4. **Correlated-failure breakers** — (1) the differential oracle the agent didn't produce — the whole game, and free here; (2) tests-as-spec, test files read-only to the implementer; (3) a held-out request corpus the agent never sees; (4) lean on the gopter property; (9) re-run the gate yourself — never trust the agent's "benchmarks pass" summary.
5. **Tools** — gopter/rapid (property), `go test -fuzz` (fuzz), go-mutesting or gremlins (mutation), `testing.B` + benchstat (perf).
6. **Meta-checks to report** — **mutation score** via go-mutesting on the new code, *not* line coverage; flakiness check via `go test -count=20 -shuffle=on`, **flake-budget = 0**.
7. **Cross-model review** — authorship triggered it: `/converge audit` on the test suite + diff, `source_label = "ORIGINAL IMPLEMENTATION"`, `source_content = oldRoute`, `rules_list` = the test-suite rules in step 4. (Converge unavailable → one fresh-context different-model review, flagged as weaker.)
8. **Loop framing** — Tap condition: *golden corpus matches **AND** benchmark improved*. Deadlock detector: 5 no-progress iterations → escalate. Harness guard: golden corpus + benchmark baseline read-only to the implementer. Runs under `/converge verify` (or a manual loop with those three guards if converge is unavailable).
