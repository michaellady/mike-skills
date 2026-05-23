# adversarial-review (vendored)

Multi-reviewer adversarial-review transport: dispatches the same prompt to
every selected reviewer CLI in parallel (default `claude,codex,agy`; `agent`
for Cursor is opt-in), parses each reviewer's JSON verdict, and emits a merged
canonical response.

This directory is **vendored from**
[`mike-skills/adversarial-review`](https://github.com/michaellady/mike-skills/tree/main/adversarial-review)
plus the shared
[`mike-skills/llm-provider`](https://github.com/michaellady/mike-skills/tree/main/llm-provider)
module that powers the CLI dispatch. The vendoring exists so a clone of
`claude-social-media-skills` works end-to-end without a separate `mike-skills`
checkout.

## Layout

```
_shared/adversarial-review/
  SKILL.md                         # canonical skill spec (authoritative copy in mike-skills)
  README.md                        # this file
  go.mod                           # binary's module + replace → ./internal/llm-provider
  main.go                          # parallel dispatch + JSON parse + FAIL-OR merge
  main_test.go                     # unit tests for parse + merge
  sync.sh                          # pull fresh copies from upstream mike-skills
  internal/llm-provider/           # vendored from mike-skills/llm-provider/
    go.mod
    provider/provider.go           # Provider interface + Options + Error
    claude/claude.go               # claude CLI provider
    codex/codex.go                 # codex exec provider
    agent/agent.go                 # Cursor agent CLI provider (opt-in)
    agy/agy.go                     # agy CLI provider (default; replaced gemini)
```

## Build

```bash
cd _shared/adversarial-review
go build -o adversarial-review .
```

The compiled binary is gitignored (each user builds locally).

## Usage

```bash
printf '%s' "$ASSEMBLED_PROMPT" | _shared/adversarial-review/adversarial-review
```

Flags:

- `--reviewers <csv>` — which reviewers to dispatch (default `claude,codex,agy`; `agent` is opt-in)
- `--prompt-file PATH` — read prompt from file instead of stdin
- `--timeout SECONDS` — per-reviewer timeout (default 300)
- `--quiet` — suppress provider heartbeat lines on stderr

To add the opt-in `agent` reviewer (needs Cursor quota):

```bash
# Add Cursor agent to the default trio
printf '%s' "$ASSEMBLED_PROMPT" | _shared/adversarial-review/adversarial-review \
  --reviewers claude,codex,agy,agent

# Or scope down to a specific subset
printf '%s' "$ASSEMBLED_PROMPT" | _shared/adversarial-review/adversarial-review \
  --reviewers claude,codex
```

See [SKILL.md](SKILL.md) for the contract (input requirements, output JSON
shape, merge rule, when to use, when not to use).

## Sync with upstream

When `mike-skills/adversarial-review` or `mike-skills/llm-provider` changes,
re-vendor:

```bash
_shared/adversarial-review/sync.sh             # interactive (diffs + prompts)
_shared/adversarial-review/sync.sh --check     # exit 1 if drift detected
_shared/adversarial-review/sync.sh --apply     # overwrite without prompt
```

The script honors `MIKE_SKILLS_DIR` if your upstream checkout isn't at
`~/dev/mike-skills`.

## Tests

```bash
go test ./...                         # pure logic only — fast, no CLIs invoked
./smoke.sh                            # end-to-end against every CLI provider (burns a few cents)
./smoke.sh claude codex               # specific N-way combo
```

Pure-logic Go tests cover JSON parsing, merge rule, and issue dedup. They
do NOT invoke the actual `claude` / `codex` / `agent` / `agy` CLIs
(so they don't burn tokens or require any CLI installed for CI). End-to-end
provider smoke is a separate `./smoke.sh` — required after every change to
provider code or after upgrading a provider CLI. Caught the cursor `agent`
provider needing `--model auto` on free plans (2026-05-03).
