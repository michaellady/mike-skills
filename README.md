# mike-skills

A workspace of [Claude Code](https://claude.com/claude-code) **skills** — focused, reusable capabilities that Claude loads on demand. Each top-level directory is one skill, defined by a `SKILL.md` (frontmatter + instructions); a few are backed by a small Go binary for the deterministic "transport" parts.

These are the source of truth; the installed copies under `~/.claude/skills/` are symlinks back into this repo (see [Installing](#installing)).

## Skills

### Multi-AI review & verification

| Skill | What it does |
|---|---|
| [converge](converge/) | Claude, Codex, agy, and the Cursor models Composer 2.5 & Grok Build iterate on an artifact until they converge — or surface a deadlock for you to arbitrate. Five modes: `plan`, `implement`, `verify`, `review`, `audit` (fresh-eyes adversarial review). Backed by a Go binary. |
| [hegelian-dialectic](hegelian-dialectic/) | Works an artifact through an explicit thesis → antithesis → synthesis loop (Claude + Codex) until a transcendent position emerges or the dialectic stalls. Same artifact types as `converge`, different rhythm. |
| [maximize-verification](maximize-verification/) | Stacks every independent check a piece of code admits (differential, property, metamorphic, fuzz, concurrency, static, cross-model) on the strongest available oracle — built to break the correlated-failure trap of one agent writing both code and its tests. |
| [verified-ship](verified-ship/) | Hard READ-gate state machine for a gated ship pipeline (local verify → commit → push → CI → audit → auto-merge): every gate's real result must be read before advancing, and "run the audit" can never share a turn with "arm the merge." Stops a check being *claimed* without being *read*. |
| [probe-before-wire](probe-before-wire/) | Before baking an external config value (model id, endpoint, ARN, region, image tag, credential) into code or a deploy, invoke the real dependency once against the target account and read the response — catch a dead/renamed/permission-blocked value at the source, not in production. `maximize-verification` applied to config/infra. |
| [ladder-the-failure](ladder-the-failure/) | When a wired integration is silently/opaquely failing, ladder down the ownership stack (your wiring → env → IAM → an org SCP → an account-level subscription/enablement → the provider's response format), verify the layers you own are actually applied, and get to the ONE authoritative signal — the real log line, an elevated-creds probe, latency-as-discriminator — before attributing blame or attempting a fix. The debugging counterpart to `probe-before-wire`. |

### Porting (JVM → Rust)

A phase family for behavior-preserving ports of legacy Java / Kotlin / JVM code to Rust, whose deliverable is an evidence bundle an auditor can check — not a "verified" slogan. Each phase reads the previous phase's file and writes its own under `.port/`, so the methodology is traversed rather than remembered. Start with [port-jvm-to-rust](port-jvm-to-rust/) and its [REFERENCE.md](port-jvm-to-rust/REFERENCE.md).

| Skill | Phase | What it does |
|---|---|---|
| [port-jvm-to-rust](port-jvm-to-rust/) | orchestrator | The three failure modes of a port, the confidence chain, the fixed claim vocabulary, the gate order, and where Specula fits (defect discovery, never assurance). |
| [port-qualify](port-qualify/) | 0 | Is the JVM actually the cost (CPU, memory, tail)? Is the code port-shaped (no reflection/JNI/JMM in the core)? Measured go / no-go. |
| [port-surface](port-surface/) | 1 | The observable-surface contract (what "same output" means, signed by the owner) and the dependency boundary map (port / wrap / replace / out-of-surface). |
| [port-capture](port-capture/) | 2 | The impression: per-method and sequence corpora by bytecode instrumentation, tiered public / hidden / sealed, captured twice on different JDKs and hosts with every nondeterministic field classified. |
| [port-alphabet](port-alphabet/) | 3 | Branch coverage of the *original* under the corpus, coverage-guided fuzzing to saturation, and the published unreached list — the denominator for every later number. |
| [port-audit](port-audit/) | 4 | Defects in the original (archaeology, fuzz crashes, Specula for protocol-shaped targets), each with an owner's preserve / fix ruling; the mutation catalogue that is not derived from the contract. |
| [port-model](port-model/) | 5 | The total, deterministic, executable reference model plus `DECISIONS.md`; the TLA+ invariant spec; the refinement link check with a canary. |
| [port-literal](port-literal/) | 6 | Structure-preserving transliteration driven to per-method byte-exact conformance and clean differential agreement, in dependency order. |
| [port-optimize](port-optimize/) | 7 | The real port — lock lifted, representations flattened — with each unit proven equivalent to its literal counterpart under Kani, planted-defect controls required to fail, and performance/memory as pass/fail goldens. |
| [port-link](port-link/) | 8 | Trace validation of the Rust against the TLA+ model and interleaving search for the concurrency class no sequential oracle can score. |
| [port-calibrate](port-calibrate/) | 9 | The per-link mutation kill table with cost in launches, from a catalogue not derived from the model. |
| [port-ceiling](port-ceiling/) | 10 | The headline report: weakest link first, every claim with its bound and canary, what the ladder cannot see, the rulings, the numbers, the cutover readiness ladder. |
| [port-learn](port-learn/) | standing | Files findings at every gate, classifies rediscoveries vs new rules vs tool gaps, and edits this family with the finding cited — the rule goes into the step where it fires, never a notes directory. |

### Skill lifecycle & meta

| Skill | What it does |
|---|---|
| [new-skill](new-skill/) | Scaffold a new Claude Code skill. |
| [install-skill-framework](install-skill-framework/) | Install a skill or skill framework from a GitHub URL (superpowers, gsd, bmad, speckit, openspec, …). |
| [uninstall-skill](uninstall-skill/) | Remove a skill or framework and clean up broken/orphan installs. |
| [skill-audit](skill-audit/) | Inventory installed skills — find duplicates, orphan symlinks, trigger overlaps, and unknown-origin skills. |
| [primitive-test](primitive-test/) | Decide whether a capability belongs in code or in the prompt, via the three-condition Primitive Test (Atomicity, Bitter Lesson, ZFC). |
| [review-chats](review-chats/) | Mine your Claude Code chat history for recurring patterns, forgotten threads, and skill-abstraction candidates. |

### Dev utilities

| Skill | What it does |
|---|---|
| [repo-cache](repo-cache/) | Shallow-clone a referenced GitHub repo locally so exploration uses fast Read/Grep/find instead of repeated `gh api` calls. |

## Repository layout

```
mike-skills/
├── <skill>/SKILL.md        # one directory per skill
├── converge/go/            # Go source for the converge transport binary
│   └── build.sh            # builds converge/bin/converge
└── llm-provider/           # shared Go module: one Provider per LLM CLI
    ├── provider/           #   the Provider interface + Options
    ├── claude/ codex/ agy/ agent/ gemini/
    └── go.mod
```

`llm-provider` is the shared module the Go-backed skills import to invoke each model's CLI (`claude`, `codex`, `agy`, and the Cursor `agent` CLI) behind a single `Provider` interface.

## Installing

Skills are picked up from `~/.claude/skills/`. Symlink the ones you want so edits in this repo take effect immediately:

```sh
ln -s "$PWD/converge" ~/.claude/skills/converge
# …repeat per skill, or for all of them:
for d in */; do
  [ -f "$d/SKILL.md" ] && ln -sfn "$PWD/${d%/}" ~/.claude/skills/"${d%/}"
done
```

Once installed, invoke a skill in Claude Code with its slash command (e.g. `/converge`, `/new-skill`) or just describe the task — Claude triggers the matching skill automatically.

## Building the Go-backed skills

Skills that ship a binary build with their own script (needs Go 1.25+, no external deps):

```sh
cd converge && bash build.sh   # → converge/bin/converge
```

Run the test suite for a Go-backed skill from its module root:

```sh
cd converge/go && go test ./...
```

## Authoring a new skill

Use the [new-skill](new-skill/) skill (`/new-skill`) to scaffold the directory and `SKILL.md`. When deciding what logic belongs in a binary versus the prompt, run it through [primitive-test](primitive-test/).
