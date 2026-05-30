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
