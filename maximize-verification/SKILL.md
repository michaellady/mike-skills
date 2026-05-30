---
name: maximize-verification
description: Use when you need to maximally verify a piece of code — especially AI-generated code — by stacking every independent check it admits (differential/property/metamorphic/fuzz/concurrency/static/cross-model), anchored on the strongest available oracle, and breaking the correlated-failure trap (one agent wrote both code and tests). Triggers — "how do I maximally verify this", "make this bulletproof", "how should I test this", "make this safe to optimize/refactor with AI", "what oracle should I use", "I don't trust the agent's tests", "maximize verification", "is my test suite actually verifying anything".
user_invocable: true
---

# maximize-verification

Given a piece of code, **maximize the verification signal on it** — extract as many *independent* checks of correctness as the code admits. Reliability compounds only with checks whose failure modes are uncorrelated: two tests that fail together give you one test's worth of assurance, but a differential oracle, a property generator, a race detector, and a different-model reviewer fail for *different* reasons, so stacking them multiplies coverage of the ways the code could be wrong. The job is to turn on every independent lens the code admits and then prove the stack itself catches bugs.

The single highest-leverage move is to anchor the stack on **an oracle the agent didn't produce.** For a rewrite/optimization the previous implementation is a free, perfect one, and equivalence-to-a-trusted-reference survives even total domain ignorance — an agent can rewrite a tropical-semiring hot path it doesn't understand as long as "correct" means "matches the old implementation." This is also why **correlated failure** is the failure mode unique to agent work: when one agent writes both the code and its tests, a spec misunderstanding is encoded *identically* in both and the green check is meaningless. Maximizing verification *is* maximizing independence from that single point of failure.

So the procedure is always: **establish the strongest oracle available (Step 1), then layer every other independent check the code admits, ordered by leverage (Step 2+).** Maximal means maximal *independent signal* — not running every tool blindly, and never chasing line coverage (the classic trap; see Step 5).

This skill *builds the verification stack*. It does not run the loop ([converge](/Users/mikelady/dev/mike-skills/converge/SKILL.md) does) and it does not execute the cross-model audit itself ([`converge audit`](/Users/mikelady/dev/mike-skills/converge/SKILL.md) is the primitive it delegates to). Full taxonomy and tool tables live in [REFERENCE.md](./REFERENCE.md) — read it when you need the strategy/tool catalog; this file is the operational procedure.

## When to Use

Use when:
- You want to verify a piece of code as thoroughly as possible — stack every independent check it admits, not just add a test or two.
- Optimizing, refactoring, or porting code where an old/reference implementation exists (the highest-value case — a free, perfect oracle).
- An agent wrote (or will write) both the code and its tests and you don't trust the green check.
- Someone asks "how do I maximally verify this?", "how should I test this?", "what oracle should I use?", or "is my test suite actually verifying anything?"
- A test suite reports high coverage but you suspect it isn't catching bugs.

Do NOT use for:
- Trivial one-line changes — overhead exceeds benefit.
- Cases where the user is the sole source of truth and there's no external reference to audit against (then it's a judgment call, not a verification problem).
- Actually *running* a verification loop — that's `/converge verify`. This skill decides what the loop should check.

## Step 0 — Diagnose the change

Establish these five facts. Infer from context where you can; ask **at most one** AskUserQuestion for the rest.

1. **Change type** — greenfield / refactor-or-optimize-with-existing-impl / port (language A→B) / legacy-modification.
2. **Oracle availability** — is there an old implementation? a reference implementation? a formal spec? an external golden dataset? or nothing?
3. **Authorship** — did/will the *same* agent write both the code and the tests? (This is the correlated-failure flag.)
4. **Nature** — concurrency-/transaction-heavy? performance-critical? parses untrusted input? distributed/fault-tolerant? (Each opens an additional independent verification layer in Step 2.)
5. **Current verification** — none / example tests / property tests / a reported coverage number.

## Step 1 — Establish the strongest oracle (the foundation)

The oracle you can establish is the highest-leverage layer in the whole stack, so build it first — it anchors everything else. This is not "pick one and stop"; it is "secure the strongest correctness reference available, then keep layering in Step 2." Find your row, establish that oracle, and **upgrade your situation where you can** (e.g. untested legacy → *capture* a characterization oracle so you're no longer in the "nothing" case).

| You have… | Strongest oracle / spine | Why it's the anchor |
|---|---|---|
| **Old/reference impl** (refactor / optimize / port) | **Differential / conformance over golden artifacts.** Capture golden outputs from the old impl (this *is* characterization/pinning — pin existing behavior even if "wrong"), run the new impl on the same inputs, diff. | The old impl is a free, perfect oracle. The agent literally cannot "agree with itself" into a bug. Domain ignorance is survivable. |
| **Untested legacy code you're about to change** (no spec, no tests, but the running code exists) | **Characterization/pinning first, then differential.** The legacy module's *current behavior* IS the oracle — capture it as pinning tests **before touching anything**, then diff the modified code against those goldens. Do NOT route this to "greenfield." | Even with no spec and no tests, running code is a reference. Pinning it gives you an oracle the agent didn't author — the strongest breaker — for the price of a capture run. |
| **Formal spec** (no impl, but a written contract) | **Spec-as-oracle:** property + metamorphic tests derived from the spec, plus static verification (types, exhaustiveness, TLA+/Alloy where the domain warrants). | The spec defines correctness independent of any implementation. |
| **No absolute oracle, but known relations** between outputs | **Metamorphic.** Test relations instead of values: `sort` is permutation-invariant, `encode∘decode` is identity, `f(2x) == 2·f(x)`. | You can verify without knowing the answer. |
| **Nothing** (greenfield) | **No equivalence oracle — say so plainly. Human spec review is unavoidable.** Compensate by maximizing the *other* layers hardest: tests-as-spec + property/metamorphic + held-out suite + cross-model review + static verification. | No reference exists; green checks here are genuinely weaker, so the burden shifts entirely onto independent layers and human review. |

State the strongest oracle you established (or that none exists) explicitly in the output — it sets the ceiling on how much the rest of the stack can buy you.

## Step 2 — Layer every independent check the code admits

This is where verification is *maximized*. Each layer below is a distinct failure-mode lens — it catches a class of bug the others structurally cannot. **Evaluate every layer**; turn each one ON when it applies *and* its independent signal justifies the cost — they're additive, never alternatives. Walk the table top to bottom and for each one decide *on / off / why*, so the output is an explicit, cost-aware maximal stack rather than a single technique. (The table order is a walkthrough checklist, not a priority — once you have the "on" set, order it by *leverage* per the note after the table.)

| Independent layer | What it catches that no other layer does | Turn on when |
|---|---|---|
| **Differential / conformance** (the Step 1 oracle) | Any semantic divergence from the trusted reference — including bugs you'd never think to assert | An oracle exists (rewrite / optimize / port / pinned legacy). The strongest layer; always on if available. |
| **N-version / redundancy** | Bugs shared by a single implementation+test pair — broken by *independently produced* implementations that diff or majority-vote against each other | No single trusted oracle exists but you can afford ≥2 independent impls (or generators). Generalizes differential; cross-model review (Step 4) is the LLM-authored form. Independence is the whole point — shared bugs defeat it. |
| **Property-based** | Edge cases you never enumerated; invariant violations on machine-generated inputs | Almost always — any function with a statable invariant (round-trip, idempotence, ordering, bounds). Agents are weakest exactly here. |
| **Stateful / model-based** | Lifecycle and ordering bugs that surface only across a *sequence* of operations — invisible to one-shot property tests | Any stateful unit (cache, queue, DB, allocator, session). Drive operation sequences against a model and assert invariants after each step (Hypothesis `RuleBasedStateMachine`, proptest-state-machine, fast-check model-based, jqwik stateful). |
| **Metamorphic** | Bugs visible only as broken *relations* between outputs, with no oracle for absolute values | Known relations exist (`sort` permutation-invariance, `f(2x)==2·f(x)`, encode/decode). Critical when there is no equivalence oracle. |
| **Statistical / distributional** | A wrong output *distribution* for stochastic/ML/randomized/LLM code (mean/variance/quantiles, calibration, drift) — invisible to deterministic differential/property/metamorphic, which assume a fixed output | Output is a random variable. Assert the distribution (KS/χ² vs an **independent reference the agent didn't produce** — trusted old impl, curated/held-out data, reviewed prod telemetry; significance between variants) with a declared protocol (sample size/power, alpha, effect-size, seed) + a re-baselining rotation. A self-generated golden is weak → non-blocking monitoring. Tools: scipy.stats, Hypothesis stats, deepchecks, evidently. |
| **Fuzzing** | Crashes, panics, hangs, UB, and unhandled inputs across the whole input space | Code parses/decodes structured or untrusted input, or has a large input surface. Multiplies under sanitizers + runtime contracts. |
| **Sanitizers / dynamic analysis** | Memory-safety + undefined-behavior bugs — use-after-free, OOB, uninit reads, leaks, UB — that produce correct-looking output until they don't | Native/`unsafe`/cgo code. Run tests *and* fuzzers under ASan/UBSan/MSan/LeakSanitizer, Valgrind, or Miri (Rust). Distinct from the concurrency row's TSan. |
| **Symbolic / concolic + bounded model checking** | Inputs that reach a specific path or trip an assertion — found by *solving* path constraints, not guessing — plus absence-of-violation proofs within a bound | High-stakes pure logic where random fuzzing is too weak. Verifies the *actual implementation* (Kani for Rust, CBMC for C, KLEE, CrossHair for Py), unlike the abstraction-level model checking in the static row. |
| **Concurrency (race + interleaving + DST)** | Data races, lost updates, ordering bugs, deadlocks — invisible to single-threaded tests | Any shared mutable state, transactions, or concurrency. Use `-race`/TSan + Loom/jcstress + deterministic simulation (madsim, turmoil, Antithesis). A non-reproducible failure is undebuggable. |
| **Static verification** | Whole bug *classes* eliminated without executing — type errors, non-exhaustive matches, nullability, lint violations; and with annotations, *proved* properties (deductive: Dafny/Verus/Gobra; model checking: TLA+/Alloy) | Always. The cheapest, most uncheatable signal; turn the compiler/linter into a gate with `-Werror`. Reach for proof/model-checking on protocol- or safety-critical cores. |
| **Contract testing** | Mismatches at a *service/API boundary* — consumer expectations vs provider responses — without standing up both sides | Code talks across a service or library boundary you don't own both ends of (Pact-style consumer-driven contracts). |
| **Runtime contracts / design-by-contract** | Precondition/postcondition/invariant violations *on every execution* — in production and inside every fuzz/property case | A non-trivial invariant is worth enforcing continuously. Embed assertions/contracts (`debug_assert!`, icontract, Ada/SPARK, Clojure spec); they multiply fuzzing and property tests. |
| **Performance (bench / load / soak)** | Throughput/latency/memory regressions — an isomorphic-but-*slower* rewrite is a **failed** optimization | Performance-sensitive code, or any optimization. Make the perf delta a first-class pass/fail gate, not a footnote. |
| **Compatibility / migration** | Breakage across versions — new impl can't read old serialized data, wire formats drift, a schema migration loses or corrupts data, or the public **API/ABI surface** changes in an unintended semver-breaking way | Ports/rewrites touching persisted state, wire formats, or exported interfaces. Round-trip old↔new data; test upgrade *and* downgrade and forward/backward schema compat; golden the public API/ABI signature and fail on unintended breaks (cargo-semver-checks, go-apidiff, api-extractor, japicmp). |
| **Combinatorial / pairwise** | Interaction bugs in large configuration spaces that single-variable tests miss, without the full cross-product | Many independent options/feature-flags/enums interact. Generate an all-pairs (or n-wise) covering set (PICT, ACTS, allpairspy). |
| **Acceptance / BDD (executable spec)** | Divergence from agreed *user-facing behavior* that unit/property tests don't express — plus a business-readable spec a human can actually review | User-facing behavior with stateable acceptance criteria. Write the spec as executable scenarios (Gherkin/Cucumber, behave, SpecFlow); it doubles as the human-reviewed "tests are the spec" artifact. |
| **Visual regression** | Layout/rendering/pixel diffs invisible to DOM- or logic-level assertions | UI or any rendered/graphical output. Snapshot the rendered result and diff (Percy, Chromatic, Playwright/Storybook screenshots). |
| **Chaos / fault injection** | Failure to degrade gracefully under killed processes, latency, partial outages | Distributed or fault-tolerant systems. |
| **Observability / telemetry-emission** | Silent monitoring failure — the error path doesn't emit the metric, the span is missing — while functional output is correct, so production failures go unseen | Code whose failures must be observable (prod services). Assert the code *emits* the critical event + approved dimensions/cardinality via an in-memory exporter (sampling off). Alert/SLO *firing* is monitoring config (out of scope); never snapshot full log text or vendor formatting (those rot). Tools: OpenTelemetry test exporters, structured-log capture. |
| **Shadow / production differential** | Divergences that surface only on *real* traffic and state — the production-time form of the differential oracle | Cutting over a rewrite/optimization in a live system. Mirror real traffic to old + new and diff before promoting (GitHub Scientist, Twitter Diffy, dark launch / canary). |
| **Cross-model adversarial review** (Step 4) | What the *implementer model's* blind spot missed — a different family fails differently | Whenever an agent wrote both code and tests. See Step 4. |

**Security is an adjacent axis** — for anything exposed, multi-tenant, or handling untrusted input/secrets, add **developer-authored abuse-case / negative tests** (authz & tenant-isolation bypass, input-validation boundaries, business-logic bypass, secret non-leakage) *plus* out-of-band scanners (SAST/taint, DAST, dependency-SCA, secret scanning) and fuzzing-for-vulnerabilities; they catch exploitability, which correctness layers don't. (The abuse-case tests are mechanically negative example/property tests — a security *mindset* applied through existing lenses, not a separate one.)

Maximal ≠ exhaustive busywork. Prioritize by leverage (oracle/differential → static + runtime contracts → property/stateful/metamorphic → the nature-specific layers: fuzz+sanitizers / concurrency / symbolic / perf / compat → cross-model review) and stop adding layers when the next one's *independent* signal isn't worth its cost. The goal is to maximize uncorrelated coverage of the failure space, not to run the longest possible tool list. Then **validate the assembled stack with mutation testing (Step 5)** — it measures whether the stack would catch a bug, not the code, so it's the meta-check, not a layer in the table above.

## Step 3 — Correlated-failure breakers

Apply these **whenever one agent writes both code and tests** (Step 0.3 flag). Listed in priority order — the earlier ones are stronger. Recommend as many as the stakes justify.

1. **Use an oracle the agent didn't produce** (Step 1) — the strongest lever, and the whole point. For optimization/refactor this is free.
2. **Make tests the spec, and protect them** — a human reviews the *test suite* (not every impl line). Make test files read-only to the implementing agent, or split test-writing and code-writing into separate agents/contexts. Otherwise the agent reward-hacks: weakens assertions, adds `skip`/`xfail`, special-cases inputs, hardcodes expected values. **Always diff the test files in review.**
3. **Hold out tests the agent never sees** — a private suite that runs only *after* the agent declares done. This is the generalization check against overfitting to the visible suite.
4. **Lean on property-based and metamorphic tests** — agents are systematically good at the happy path and bad at edge cases; generators don't share that blind spot, and properties don't require the agent to have enumerated anything.
5. **Cross-model adversarial review** — a *different* model has *different* failure modes, so it catches what the implementer's blind spot missed. This is **Step 4 below** (the wire-in). Same-model review mostly rationalizes.
6. **Shrink the blast radius** — small diffs, pure functions, narrow typed interfaces. An isomorphic 30-line change is verifiable; a 600-line one is hope. This is why an incremental hot-path strategy beats a big-bang rewrite.
7. **Push correctness into the compiler** — strong types, exhaustiveness checks, linters with `-Werror`. Type errors are the highest-signal feedback you can give an agent: fast, precise, uncheatable.
8. **Determinism** — seed everything; deterministic simulation for concurrency. A non-reproducible failure is one the agent can't debug and one whose fix you can't trust.
9. **When you delegate the work, run the gate yourself — never trust the implementer's own green check.** If a subagent implemented (or you handed off the mechanical mirroring), its "100% / all tests pass" summary is the *implementer* reporting on its own work — the exact correlated-failure trap. Independently re-run the full gate (the suite, the coverage check, the build) in your own context and read the real numbers before you act on them. The point of delegation is parallelism, not a second source of truth; the verifier must be independent of the implementer. (Same reason cross-model review beats same-model review — the green check has to come from outside the thing that produced the code.)

## Step 4 — Wire in cross-model review

When an agent wrote both code and tests (and the stakes justify it), have a **different model family** audit the artifacts. Don't rebuild this — delegate to `/converge audit` (it fans the same prompt out to claude + codex + agy in parallel and merges with FAIL-OR). See the `audit` mode in [converge/SKILL.md](/Users/mikelady/dev/mike-skills/converge/SKILL.md) for the full contract.

Map the verification artifacts onto its required inputs:

| converge audit input | Value for verification review |
|---|---|
| `source_label` | `"TRUSTED REFERENCE"` / `"ORIGINAL IMPLEMENTATION"` / `"SPEC"` |
| `source_content` | The oracle: the old impl, the spec, or the requirements |
| `skill_name` | `maximize-verification` |
| `artifact_name` | `test` (audit the suite) and/or `patch` (audit the diff) |
| `rules_list` | The verification requirements the artifacts MUST satisfy (see below) |
| `drafts` | The generated test files and/or the diff, each with an `id` |

Suggested `rules_list` for auditing an agent's test suite (this is where reward-hacking hides):
- "Every test asserts on outputs — it does not merely call the code."
- "No `skip`/`xfail`/commented-out assertions, and no `@Disabled`/`it.skip`."
- "Edge cases and error paths are covered, not only the happy path."
- "No hardcoded expected values that special-case a specific input to force a pass."
- "Any property/invariant the suite claims to test actually holds for generated inputs."

`converge audit` takes **one composed text prompt on stdin** — not JSON and not flags. So the fields above are *sections you write into that single prompt*, not separate arguments: assemble `$ASSEMBLED_PROMPT` using converge audit's Phase 2 scaffold — open with the `source_label` + `source_content`, name the `skill_name` and `artifact_name`, list the `rules_list`, then the `drafts` as a numbered list (the binary owns transport + merge only; composing the prompt is the caller's job). Then invoke (build converge once if needed):

```bash
# bash ~/dev/mike-skills/converge/build.sh   # one-time, if bin/converge is missing
printf '%s' "$ASSEMBLED_PROMPT" | ~/dev/mike-skills/converge/bin/converge audit
```

Honor its contract: FAIL-OR (any reviewer's FAIL → FAIL), and **caller-side loop protection** — if the same FAIL signature recurs 3× across revise/re-audit cycles, stop and escalate to the human. A persistently-failing assertion often means the agent believes the rule is wrong (a judgment call), not a fixable bug.

## Step 5 — Meta-checks: is the suite itself trustworthy? (the coverage trap + flakiness)

These measure the *verification*, not the code. Two distinct meta-checks:

- **Report the mutation score, not line coverage.** Coverage measures what was *executed*, not *verified* — a test with zero assertions gives 100% line coverage. Mutation testing injects bugs and checks whether the suite catches them — whether your tests would *catch* a defect, which is what you actually care about. If you report one number for an agent-generated suite, make it the mutation score. Tools: Stryker (JS/TS), mutmut (Python), PIT (Java), cargo-mutants (Rust). See [REFERENCE.md](./REFERENCE.md).
- **Detect flakiness / nondeterminism** (sibling to mutation score — measures verdict *stability* vs catch-power). A suite that passes/fails nondeterministically is coin-flip green and voids every other layer. Rerun under randomized order/seed/clock/parallelism to *expose* instability; **default flake-budget = 0** for a blocking gate — the budget is not a masking escape hatch, and any quarantined exception must stay a *separate failing* stability signal until it's fixed (never silently dropped). Tools: pytest-randomly + reruns, `go test -count/-shuffle`, surefire rerun.
- Recommend MC/DC only where the domain demands it (DO-178C / avionics-grade). It's genuinely rigorous but overkill for ordinary work.

## Step 6 — Frame the loop with a legible tap condition

If the verification will run as an iterative agent loop, it needs a clean termination signal or it thrashes — or, worse, "wins" by quietly mutating the harness. Recommend:

- **Tap condition (when to stop):** a single legible signal, e.g. *golden artifacts match **AND** benchmarks improved*. A loop without a legible tap condition doesn't converge.
- **Deadlock detector:** N iterations of no progress → escalate to human. Without it, agents grind forever.
- **Guard the harness:** the implementing agent must not be able to edit the tests or golden files (checksum/read-only/diff-on-review). "Passing" by mutating the oracle is the canonical cheat.

Then hand the actual loop to existing machinery — don't rebuild it:
- `/converge verify` — iterative test-strengthening with built-in N-round bound + deadlock detection + smoke-check (treats the verifier toolchain as the oracle).
- `/converge implement` — small-diff implementation loop with the same convergence/deadlock machinery.

See [converge/SKILL.md](/Users/mikelady/dev/mike-skills/converge/SKILL.md).

## Output format

Produce a tight **Maximal Verification Stack** for *this* code — every independent layer it admits, ordered by leverage:

1. **Diagnosis** — change type, oracle available, authorship (correlated-failure flag), nature, current verification.
2. **Anchor oracle** — the strongest oracle established in Step 1 (or "none — greenfield"), with the spine technique named.
3. **The stack** — walk the **full Step 2 layer table** and mark each *on / off / why*. The "on" set, ordered by leverage, IS the maximal harness.
4. **Correlated-failure breakers** — the Step 3 levers to apply, in priority order, concrete to this code.
5. **Tools** — language-matched, from REFERENCE.md, one per active layer (property, fuzz, mutation, concurrency).
6. **Meta-checks to report** — mutation score (with the tool name), never line coverage alone; plus a flakiness/verdict-stability check (flake-budget = 0 for blocking gates).
7. **Cross-model review** — the Step 4 wire-in, if authorship triggered it.
8. **Loop framing** — the tap condition, deadlock detector, harness guard, and which `/converge` mode runs it.

Keep it scannable — a real stack fits on one screen. Call out explicitly which layers you turned *off* and why, so the reader sees the maximization was deliberate, not partial.

## Relationship to neighbors / What this is NOT

- `maximize-verification` **builds the maximal verification stack**; `/converge verify` **runs the loop**; `/converge audit` **is the cross-model audit primitive** it delegates to; `primitive-test` is the analogous code-vs-prompt advisor.
- NOT a test runner, NOT a coverage tool, NOT a mutation-testing engine — it recommends those, it doesn't reimplement them.
- NOT a substitute for human spec review on greenfield code. When there's no oracle, it says so; it does not pretend a green suite means correct.
- NOT a way to launder a bad rewrite into a "verified" one. Equivalence to a trusted reference is strong; two agents agreeing is not.

## Common Mistakes

- **Treating line coverage as the reliability number.** It measures execution, not verification. Use mutation score.
- **Letting the implementing agent edit the tests or golden files.** That's how the harness gets gamed. Protect them.
- **Recommending a spine with no oracle and calling it safe.** Greenfield has an unsolved oracle problem — name the human-review requirement.
- **Passing compose-phase context to the cross-model reviewer.** Fresh eyes are the point; see converge `audit` mode's rules.
- **Running a loop with no tap condition or deadlock detector.** It either thrashes or cheats.
