---
title: "The Primitive Test"
source: "Gas City SDK design doc — adapted here for skill design (prompt vs code)"
---

Decision framework for whether a capability belongs in the **code layer**
(scripts, helpers, SDK functions) or in the **prompt layer** (a skill's
SKILL.md, an agent's instructions, the model's reasoning at runtime).

## The three necessary conditions

A capability belongs in code **only if all three hold.** If any condition
fails, it belongs in the prompt.

### 1. Atomicity — can it be done safely without races?

If two concurrent invocations could corrupt state or violate invariants,
the code layer must provide the atomic version. If it's naturally
idempotent or the underlying tool already handles concurrency
(SQL transactions, INSERT IGNORE, atomic FS rename, etc.), the prompt
can call the underlying tool directly.

**Questions to ask:**
- Could two agents/sessions hit this operation simultaneously?
- Does the underlying tool (git, sqlite, the OS) already provide atomicity?
- Is there a read-check-write pattern that could race?

**Examples:**
- `git commit` → already atomic from the tool → prompt layer
- "create plan file if not exists" → naturally idempotent if using `O_EXCL` → either layer fine
- "claim a slot from a shared queue" → read-check-write race → code primitive needed
- "two skills writing the same log file" → needs append-only or locking → code

### 2. Bitter Lesson — does it become MORE useful as models improve?

If a smarter model would do it better directly from the prompt, it fails
the Bitter Lesson test and belongs in the prompt. If it's pure plumbing
that models will always delegate to (and never improve upon), it's code.

**The test:** Imagine a model 10× more capable. Does this capability
become **less** necessary as a primitive (→ prompt) or **exactly as**
necessary (→ code)?

**Examples:**
- "Decide whether to retry a flaky tool call" → judgment → prompt
- "Run a subprocess and capture stdout/stderr" → plumbing → code
- "Decide which test framework to use" → judgment → prompt
- "Truncate a string at byte boundary N" → plumbing → code

### 3. ZFC — is it transport or cognition?

If implementing it requires a judgment call (`if stuck then X`,
`if the diff looks dangerous, ...`), it's cognition and belongs in the
prompt. If it's pure data movement, process management, filesystem
operations, parsing, or formatting, it's transport and belongs in code.

**The test:** Does any line of the candidate implementation contain a
judgment call? If yes, that decision belongs in the prompt, not the code.

**Examples:**
- "Parse JSONL and emit final assistant message" → transport → code
- "Decide whether codex's critique is more rigorous than claude's" → cognition → prompt
- "Detect project type from `go.mod`/`Cargo.toml`/`package.json`" → transport → code
- "Decide what to do when smoke check fails twice" → cognition → prompt

## Applying the framework

### Decision table template

| Capability | Atomicity needed? | Bitter Lesson pass? | ZFC pass? | Verdict |
|---|---|---|---|---|
| ... | Could it race? | Does a smarter model still need this primitive? | Pure transport? | All three pass → **code** |

### Common verdicts (skill-design context)

**Code (all three pass):**
- CLI subprocess wrappers (run + parse output)
- File I/O scaffolding (init log header, append rows)
- Project-type detection by marker files
- Schema validation of structured payloads
- Path resolution (find most-recent file matching pattern)
- Cache management (clone-if-missing, fetch-if-stale)

**Prompt (at least one fails):**
- Critique generation (fails Bitter Lesson)
- Convergence/deadlock judgment (fails ZFC)
- Decision about when to auto-apply vs preview an edit (fails ZFC)
- Choosing which mode to enter (fails Bitter Lesson + ZFC)
- Best-argument adversarial framing (fails Bitter Lesson)

**Fix upstream (Atomicity problem in dependency):**
- A tool whose semantics should be atomic but aren't — open an issue
  there rather than wrapping it in your skill.

## The corollary: when to fix upstream vs wrap

If a capability fails the Primitive Test only because the underlying tool
has a concurrency bug, the right fix is in the tool — not a wrapper in
your skill. Wrappers exist for ergonomics (consistent API), not to paper
over bugs.

**Fix upstream when:** The tool's own semantics should be atomic but aren't.

**Wrap when:** The tool is correct but you need to compose multiple tool
calls atomically (e.g., create worktree + setup redirect with rollback if
redirect fails).
