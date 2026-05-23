# maximize-verification — REFERENCE

The full testing landscape behind the [SKILL.md](./SKILL.md) procedure. SKILL.md builds the maximal verification stack by anchoring on the strongest oracle and then layering every independent check the code admits; this file is the catalog of layers and tools it draws from. Two axes matter: **level** (what scope) and **strategy** (how you decide what's correct). Strategy is the more interesting axis — each strategy is an independent failure-mode lens, and stacking lenses with *uncorrelated* failure modes is what maximizes verification. This is where the reliability of agent work is won or lost.

---

## Testing by level

The familiar pyramid — but the boundaries matter less than people think.

- **Unit tests** — a single function/class in isolation. Fast, deterministic, cheap to run thousands of.
- **Integration tests** — real collaborators wired together (DB, queue, filesystem). Catches the wiring bugs units can't.
- **End-to-end / system tests** — the whole app through its real interface. Slow and flaky-prone, but the only thing that tests what the user actually experiences.
- **Contract tests** — verify the interface between two services independently (Pact-style consumer-driven contracts). Lets you test an integration boundary without standing up both sides.
- **Characterization / pinning tests** — Feathers' term: tests that capture *existing* behavior of legacy code before you touch it, even if that behavior is "wrong." The required first move for any refactor/port of code you don't fully understand.

---

## Testing by strategy

Levels tell you *what scope*; strategy tells you *how you decide what's correct*. This is where agent reliability lives.

- **Example-based** — the default: hand-pick inputs, assert outputs. Limited by your imagination — the core weakness for agent work, which is good at the happy path.
- **Property-based** — assert invariants over machine-generated inputs. "Reversing twice is identity," "output is always sorted," "encode/decode round-trips." The generator finds edge cases you'd never enumerate. Tools: Hypothesis (Py), fast-check (JS/TS), proptest/quickcheck (Rust), jqwik (Java), gopter/rapid (Go).
- **Metamorphic testing** — for when you have *no oracle*: test relations between outputs instead of absolute values. If `sort([3,1,2])` and `sort([2,1,3])` must be equal, you can test sorting without knowing the answer. Underused and powerful.
- **Differential / conformance testing** — run two implementations on the same inputs and diff. **This is the "golden artifacts" idea: the old implementation is the oracle for the new one.** The safest spine for refactor/optimize/port work.
- **Snapshot / golden / approval testing** — freeze a known-good output artifact, fail on any diff. Cheap to create, but rots if nobody reviews the diffs.
- **Mutation testing** — deliberately inject bugs and check whether the tests catch them. Measures *test quality*, not code coverage. Tools: Stryker (JS/TS), mutmut/cosmic-ray (Py), PIT (Java), cargo-mutants (Rust), go-mutesting (Go).
- **Fuzzing** — coverage-guided input generation that finds crashes and panics in the input space. Tools: libFuzzer, AFL++, Go native fuzzing (`go test -fuzz`), cargo-fuzz, Atheris (Py).
- **Concurrency testing** — race detectors (`-race`, ThreadSanitizer), exhaustive interleaving explorers (Loom for Rust, jcstress for Java), and deterministic simulation testing (FoundationDB-style: Antithesis, madsim, turmoil). Essential for transaction-heavy, high-concurrency targets.
- **Performance testing** — benchmarking, load, stress, soak, profiling. Part of "verify," not separate: an isomorphic-but-slower rewrite is a failed optimization.
- **Static verification** — type systems, exhaustiveness checks, linters, abstract interpretation, formal methods (TLA+, Alloy, Stateright). Verifies *without executing* — the cheapest verification there is.
- **Chaos / fault injection** — kill processes, inject latency and errors, verify graceful degradation.

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
3. **Lean on property-based and metamorphic tests.** Agents are systematically bad at edge cases and good at the happy path. Generators don't share that blind spot.
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
| **Go** | gopter, pgregory.net/rapid | `go test -fuzz` (native) | go-mutesting, gremlins | `go test -race`, `testing/synctest` |
| **Rust** | proptest, quickcheck | cargo-fuzz (libFuzzer), afl.rs | cargo-mutants | Loom, madsim, ThreadSanitizer |
| **Python** | Hypothesis | Atheris | mutmut, cosmic-ray | — (GIL); pytest-asyncio for async |
| **JS/TS** | fast-check | jsfuzz | Stryker | — |
| **Java** | jqwik | Jazzer | PIT | jcstress, ThreadSanitizer (JVM) |

Cross-language differential/conformance and golden-artifact testing are framework-agnostic — capture the old implementation's outputs over a corpus, run the new implementation on the same inputs, and diff. Deterministic simulation testing (FoundationDB-style) is available via Antithesis (hosted), madsim (Rust), and turmoil (Rust networking).
