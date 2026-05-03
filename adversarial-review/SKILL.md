---
name: adversarial-review
description: Use when about to ship drafted artifacts (posts, code, docs, plans) and want a fresh-eyes audit against source material + skill rules before user review. Spawns a clean subagent with no compose-phase context. Returns structured PASS/FAIL verdicts with cited issues. Triggers — "adversarial review this", "audit my drafts", "fresh eyes on this", "review for fabrications", "adversarial review".
user_invocable: true
---

# adversarial-review

Spawn TWO fresh reviewers in parallel — a Claude subagent and a Codex `exec` run — to audit drafted artifacts against (a) source material and (b) skill rules BEFORE the user reviews them. Neither reviewer has context from the compose phase. Their verdicts are merged: any FAIL from either reviewer → FAIL.

This is the standalone, cross-project home for the **Adversarial Review pattern** documented in [`claude-social-media-skills/PATTERNS.md#pattern-adversarial-review`](https://github.com/michaellady/claude-social-media-skills/blob/main/PATTERNS.md#pattern-adversarial-review). Other skills can invoke this directly (or apply the pattern inline with their own subagent — both paths are equivalent and **return the canonical JSON shape defined below**).

## Requirements

The skill is implemented as a Go binary (`adversarial-review`) that wraps the shared `mike-skills/llm-provider/` module. The binary fans out the SAME prompt to every selected reviewer in parallel and emits a merged JSON verdict.

**Default reviewers (`--reviewers claude,codex`):**
- `claude` — Claude Code CLI (`claude -p <prompt> --output-format stream-json`).
- `codex` — OpenAI Codex CLI (`codex exec`).

**Opt-in reviewers** (registered alongside claude+codex but excluded from the default selection — pass via `--reviewers claude,codex,agent,gemini` to enable):
- `agent` — Cursor `agent` CLI (`agent --print --output-format text <prompt>`). Tighter quotas per the Cursor plan, hence opt-in.
- `gemini` — Google `gemini` CLI (`gemini --prompt <text> --output-format text --skip-trust`). Adds a third model family for high-stakes drafts.

**Build (one-time):**

In mike-skills directly:
```bash
cd ~/dev/mike-skills/adversarial-review
go build -o adversarial-review .
```

In a downstream repo (e.g. `claude-social-media-skills`) where this skill is vendored under `_shared/adversarial-review/`:
```bash
cd <repo>/_shared/adversarial-review
go build -o adversarial-review .
```

**Verifying it works (REQUIRED after every change to provider code or after upgrading a provider CLI):**

Pure-logic Go tests cover parse + merge + dedup but DO NOT invoke the actual `claude.Run()` / `codex.Run()` / `agent.Run()` / `gemini.Run()` CLI dispatch. End-to-end smoke is mandatory before claiming a transport change works:

```bash
_shared/adversarial-review/smoke.sh                # individually + N-way default + N-way all
_shared/adversarial-review/smoke.sh claude         # just one provider
_shared/adversarial-review/smoke.sh claude codex   # specific N-way combo
```

This caught the agent provider's `--model auto` requirement on free Cursor plans on 2026-05-03 (would have shipped broken otherwise — the Go tests passed).

If a selected CLI is missing on the system (`command -v <name>` returns nothing) the binary degrades gracefully to a smaller reviewer set and lists the missing one(s) in `skipped: {<name>: "<reason>"}` in the response.

**Why dual-reviewer (default):** different model families catch different failure modes. Claude tends to flag tone/voice drift, CTA violations, and brand-voice mismatch; Codex tends to flag logical inconsistency, unsupported quantitative claims, and structural rule violations. Two independent passes ≈ catches the union of failure modes a single reviewer would miss. Adding `agent` (Cursor) is a third independent perspective — useful for high-stakes drafts where the marginal cost of one more reviewer is acceptable.

## When to Use

Use when:
- A skill has drafted social media posts, code changes, plans, or docs that are about to be shown to the user for approval
- The drafts could plausibly contain fabrications ("every leader I respect…"), rule violations (verbatim drift, missing CTA), or over-claims (inflated metrics, unsupported assertions)
- The composer agent had context that might have biased it toward rationalizing problems away
- The cost of a wrong artifact shipping is high (a fabrication on a published LinkedIn article; a paraphrased "verbatim" quote in a published Medium piece; a code change that breaks a documented invariant)

Do NOT use for:
- Trivial drafts (single-tweet edits, typo fixes) — overhead exceeds benefit
- Decisions that ARE judgment calls the user delegated (the composer's job is judgment; reviewer wouldn't catch "wrong angle")
- Cases where the user is the source of truth and there's no external source to audit against

## Required inputs (5 fields + 1 optional)

| # | Field | Required? | Description |
|---|---|---|---|
| 1 | `source_label` | required, non-empty string | Label for the source material (e.g. "SOURCE ARTICLE", "GITHUB PR", "PROJECT PLAN") |
| 2 | `source_content` | required, non-empty string | Full source material the drafts must respect (article body, PR diff, plan markdown, etc.) |
| 3 | `skill_name` | required, non-empty string | Name of the calling skill (e.g. `tease-newsletter`, `code-refactor`) |
| 4 | `artifact_name` | required, non-empty string | What each drafted item is called (e.g. "teaser", "diff", "post") — singular noun |
| 5 | `rules_list` | required, array of ≥1 strings | The rules the drafts MUST satisfy (verbatim-only, no fabrications, char limits, required CTA, etc.) |
| 6 | `drafts` | required, array of ≥1 objects, each `{id: string, content: string}` | The drafted artifacts to review. **The caller MUST assign an `id` to each draft** (e.g. `"linkedin"`, `"facebook"`, `"slide-3"`); if the caller omits ids, this skill assigns sequential `draft_0`, `draft_1`, … |
| 7 | `issue_guidance` | optional string | Hint to the reviewer about what kind of citations to include (e.g. "for verbatim drift, quote the 7+ word run that matches source"). If absent, the reviewer uses generic guidance. |

## Process

### Phase 1 — Validate inputs

Refuse to run (return the `parse_error` shape from Phase 5) if any of these checks fail:
- `source_label` missing or empty string
- `source_content` missing or empty string
- `skill_name` missing or empty string
- `artifact_name` missing or empty string
- `rules_list` missing, not an array, or empty array
- `drafts` missing, not an array, or empty array
- Any draft is missing `content` or has empty `content`

Soft warnings (don't refuse, but include in the response):
- If `len(source_content) < sum(len(d.content) for d in drafts) / 2`, source is suspiciously short relative to drafts — likely missing context.
- If any draft is missing `id`, assign `draft_0`, `draft_1`, … (zero-indexed by array position) and warn.

### Phase 2 — Construct the reviewer prompt

Substitute the inputs into this scaffold. **This is the canonical JSON return shape** — both Option A (this skill) and Option B (inline per PATTERNS.md) MUST produce this shape:

```
You are an adversarial reviewer for /<<SKILL_NAME>> drafts. Your job is to find problems before the user has to.

<<SOURCE_LABEL>>:
<<SOURCE_CONTENT>>

SKILL RULES (must be enforced):
<<RULES_LIST>>

DRAFTED <<ARTIFACT_NAME>>:
<<DRAFTS as a numbered list, each with its draft_id and content>>

For each draft, return:
- VERDICT: "PASS" or "FAIL"
- ISSUES: array of strings — specific problems with cited exact substrings.
  <<ISSUE_GUIDANCE — if absent, use "Cite exact strings from the draft and source.">>

Return ONLY this JSON object, no surrounding prose:

{
  "summary": "all_pass" or "some_fail",
  "verdicts": [
    {"draft_id": "<id from input>", "verdict": "PASS" or "FAIL", "issues": ["...", "..."]}
  ]
}
```

(The PATTERNS.md scaffold is generalized from the original `posts` framing to `drafts` to support cross-project use — code reviews, plan reviews, doc reviews. The semantic intent is unchanged.)

Do NOT include any compose-phase context (intent, history, prior drafts, why-the-composer-chose-this). Fresh eyes are the point.

### Phase 3 — Run the multi-reviewer binary

Pipe the assembled prompt into the `adversarial-review` binary. By default it spawns `claude` and `codex` CLIs in parallel via the shared `mike-skills/llm-provider/` transport, captures each reviewer's JSON, and merges them.

```bash
printf '%s' "$ASSEMBLED_PROMPT" | <repo>/_shared/adversarial-review/adversarial-review
```

Or with an on-disk prompt file + a wider reviewer set:

```bash
adversarial-review --prompt-file /tmp/review-prompt.txt --timeout 300 --reviewers claude,codex,agent
```

The binary handles, transparently:
- Parallel dispatch of every selected reviewer (process management, timeouts, heartbeat suppression)
- JSON parsing with markdown-fence + surrounding-prose tolerance
- Detection of any selected CLI missing on PATH → entry in `skipped: {<name>: "<reason>"}`
- Merge rule (FAIL-OR + issue clustering + `[r1+r2+...]` attribution by overlap)
- Emission of the canonical merged JSON on stdout

Flags:
- `--reviewers <csv>` — which reviewers to dispatch (default `claude,codex`; opt-in `agent`, `gemini`)
- `--prompt-file <path>` — read prompt from file instead of stdin
- `--timeout <seconds>` — per-reviewer timeout (default 300)
- `--quiet` — suppress provider heartbeat lines on stderr

**Caller responsibility:** assemble the prompt body using the Phase 2 scaffold. The binary only owns transport + merge — composing the prompt (which source, which rules, which drafts) stays in the calling skill's prompt because that's the cognition.

Important: the source MUST be inline in the assembled prompt body so each reviewer reads it as part of the audit, not separately via tool calls. For very large source content (>100KB), warn the caller — the prompts will be expensive but should still work.

### Phase 4 — Read the merged verdict

The binary emits exactly the canonical shape on stdout. Two reviewer-level failure modes are handled internally and surfaced via flags rather than escalated:

- **Malformed JSON from a reviewer:** that reviewer's output is captured into `raw_response` and the reviewer's name is appended to `parse_error: ["<name>"]`. The binary still emits a merged verdict using whatever reviewers DID parse cleanly.
- **All selected reviewers parse_error or skipped:** binary emits `summary: "parse_error"` with empty verdicts, exits non-zero (2).

**Merge rule (when at least one reviewer returned a valid verdict):**
- For each `draft_id`, the merged verdict is FAIL if ANY reviewer marked it FAIL; PASS only if every contributing reviewer marked it PASS.
- The merged `issues` array clusters issues that overlap across reviewers (substring overlap of ≥12 chars, case-insensitive). Each cluster is rendered as `[r1+r2+...] <issue text>` listing every reviewer that raised it (in canonical reviewer order).
- The merged `summary` is `all_pass` if every merged verdict is PASS, otherwise `some_fail`.

Why merge this way (FAIL-OR rather than majority-vote): a single reviewer catching a real fabrication is more valuable than other reviewers missing it. False positives are cheap (the composer revises and re-submits); false negatives are expensive (a fabricated quote ships).

### Phase 5 — Return verdicts to caller

**Success shape (all PASS or some FAIL):**

```json
{
  "summary": "all_pass",
  "verdicts": [
    {"draft_id": "linkedin", "verdict": "PASS", "issues": []},
    {"draft_id": "facebook", "verdict": "PASS", "issues": []}
  ],
  "reviewers": ["claude", "codex"]
}
```

```json
{
  "summary": "some_fail",
  "verdicts": [
    {"draft_id": "linkedin", "verdict": "PASS", "issues": []},
    {"draft_id": "facebook", "verdict": "FAIL", "issues": [
      "[claude+codex] Contains 'every leader I respect keeps a token from a past reskilling on their desk' — unverifiable third-party claim, source doesn't make this claim",
      "[codex] Stat '73% of teams adopt within 6 weeks' is not present in the source"
    ]}
  ],
  "reviewers": ["claude", "codex"]
}
```

The `reviewers` field lists who actually contributed (skipped or parse-errored reviewers are excluded). Each issue is prefixed `[<r1>+<r2>+...]` listing every reviewer that raised it, so the caller can see consensus vs single-reviewer flags.

**Hard-fail shape (input validation OR every selected reviewer skipped/malformed):**

```json
{
  "summary": "parse_error",
  "verdicts": [],
  "reviewers": [],
  "error": "no reviewers returned a usable verdict (all skipped, errored, or malformed JSON)",
  "raw_response": "<concatenated raw text from any reviewer that failed Phase 4>"
}
```

**Degraded shape (one reviewer unavailable on host):**

```json
{
  "summary": "all_pass",
  "verdicts": [...],
  "reviewers": ["claude"],
  "skipped": {"codex": "codex CLI not on PATH"}
}
```

The caller's job (NOT this skill's job) is to:
- If `summary == "all_pass"` → proceed to user review
- If `summary == "some_fail"` → revise drafts using the cited issues, re-invoke this skill, repeat until clean
- If `summary == "parse_error"` → fix inputs (or escalate to human if reviewer is malfunctioning)
- **Never surface FAIL items to the user** — the user should see only PASS-grade artifacts

## Caller responsibility: loop protection

This skill is a **single-call primitive** — it has no memory across invocations. Loop protection is the **caller's** responsibility, not this skill's. Recommended pattern for the caller:

- Track `consecutive_failure_signatures` across re-invocations.
- If the same FAIL issue appears 3+ times in a row, stop the revise/re-invoke loop and surface the issue to the human user — likely a sign that the composer believes the rule is wrong (judgment call), not a fixable fabrication.

The skill cannot enforce this itself; if a caller wants the skill to know about prior failures, the caller can append previous-failure summaries to `rules_list` so the reviewer sees them as context.

## Example invocations

### Newsletter use case

```json
{
  "source_label": "SOURCE ARTICLE",
  "source_content": "<full beehiiv article body>",
  "skill_name": "tease-newsletter",
  "artifact_name": "teaser",
  "rules_list": [
    "Teasers are ORIGINAL copy that summarize without spoiling the punchline.",
    "BANNED: any contiguous run of 7+ words copied from the source verbatim.",
    "BANNED: unverifiable third-party claims ('every leader I respect…').",
    "REQUIRED: every post ends with the canonical CTA."
  ],
  "issue_guidance": "For verbatim drift, quote the 7+ word run that matches source. For fabrication, quote the claim and explain what the source actually says.",
  "drafts": [
    {"id": "linkedin", "content": "<the linkedin teaser text>"},
    {"id": "facebook", "content": "<the facebook teaser text>"}
  ]
}
```

### Code-review use case

```json
{
  "source_label": "ORIGINAL FILE BEFORE CHANGES",
  "source_content": "<full pre-change file contents>",
  "skill_name": "code-refactor",
  "artifact_name": "patch",
  "rules_list": [
    "All public function signatures unchanged unless marked with // BREAKING.",
    "No new dependencies added without an explicit reason in the patch description.",
    "Tests must still cover every public function."
  ],
  "issue_guidance": "For broken signatures, cite the exact function name + before/after signatures. For new deps, cite the import statement.",
  "drafts": [
    {"id": "patch", "content": "<the diff>"}
  ]
}
```

### Plan-review use case

```json
{
  "source_label": "USER'S ORIGINAL REQUEST",
  "source_content": "<the user message that prompted the plan>",
  "skill_name": "write-plan",
  "artifact_name": "plan",
  "rules_list": [
    "Plan addresses every concrete deliverable the user mentioned.",
    "Plan does NOT add scope the user didn't ask for.",
    "Plan's verification steps are testable, not vague."
  ],
  "drafts": [
    {"id": "plan", "content": "<the plan markdown>"}
  ]
}
```

## Why this lives in mike-skills, not in claude-social-media-skills

The pattern is cross-project: it applies anywhere drafts need pre-review (newsletter promotion, code review, plan-mode reviews, even other reviewers). Putting it in `mike-skills/` makes it invokable from any project; putting it in `claude-social-media-skills/` would lock it to that domain.

The original adversarial review documentation lives in `claude-social-media-skills/PATTERNS.md` as part of the closed-loop architecture. This skill is the standalone implementation that PATTERNS.md references as "Option A".

## Common Mistakes

- **Passing compose-phase context to the reviewer.** Defeats the entire purpose. The reviewer must see only source + rules + drafts.
- **Auto-revising and shipping without user review.** This skill is a SAFETY NET, not a replacement for user review. After all PASS, the user still reviews — they just see better-quality drafts.
- **Using this for cases where the composer SHOULD be the judge.** If the user asked for "your opinion on the right framing," there's no source to audit against; this skill doesn't apply.
- **Revising in a loop without limit.** The CALLER must implement loop protection (see "Caller responsibility: loop protection" above) — this skill is a single-call primitive and can't.
- **Omitting `id` on drafts.** Without IDs, the caller can't map verdicts back to specific drafts. The skill auto-assigns `draft_0`/`draft_1`/… but caller-provided IDs are clearer.
