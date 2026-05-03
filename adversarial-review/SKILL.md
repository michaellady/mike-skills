---
name: adversarial-review
description: Use when about to ship drafted artifacts (posts, code, docs, plans) and want a fresh-eyes audit against source material + skill rules before user review. Spawns a clean subagent with no compose-phase context. Returns structured PASS/FAIL verdicts with cited issues. Triggers — "adversarial review this", "audit my drafts", "fresh eyes on this", "review for fabrications", "adversarial review".
user_invocable: true
---

# adversarial-review

Spawn TWO fresh reviewers in parallel — a Claude subagent and a Codex `exec` run — to audit drafted artifacts against (a) source material and (b) skill rules BEFORE the user reviews them. Neither reviewer has context from the compose phase. Their verdicts are merged: any FAIL from either reviewer → FAIL.

This is the standalone, cross-project home for the **Adversarial Review pattern** documented in [`claude-social-media-skills/PATTERNS.md#pattern-adversarial-review`](https://github.com/michaellady/claude-social-media-skills/blob/main/PATTERNS.md#pattern-adversarial-review). Other skills can invoke this directly (or apply the pattern inline with their own subagent — both paths are equivalent and **return the canonical JSON shape defined below**).

## Requirements

This skill requires:
- The **Task / Agent tool** (with `subagent_type: "general-purpose"`) to spawn the Claude reviewer.
- The **`codex` CLI** on PATH (`codex exec`) to spawn the Codex reviewer in parallel.

If `codex` is unavailable on the system (`command -v codex` returns nothing) the skill degrades gracefully to a Claude-only review and notes `codex_skipped: true` in the response. If the Agent tool itself isn't available, the caller should fall back to inline composition per PATTERNS.md "Option B".

**Why dual-reviewer:** different model families catch different failure modes. Claude tends to flag tone/voice drift, CTA violations, and brand-voice mismatch; Codex tends to flag logical inconsistency, unsupported quantitative claims, and structural rule violations. Two independent passes ≈ catches the union of failure modes a single reviewer would miss.

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

### Phase 3 — Spawn the reviewers (parallel: Claude + Codex)

Run both reviewers in parallel. Both see the SAME assembled prompt from Phase 2.

**Reviewer A — Claude subagent.** Use the `Agent` tool with `subagent_type: "general-purpose"`. Pass the assembled prompt as the agent's task input. The subagent runs with no inherited conversation context.

**Reviewer B — Codex `exec`.** Spawn `codex exec` in the same conversation turn (parallel to the Agent call). Pipe the assembled prompt via stdin so the prompt body is preserved exactly:

```bash
cat <<'PROMPT' | codex exec --skip-git-repo-check -
<assembled prompt from Phase 2>
PROMPT
```

(Use a unique HEREDOC sentinel if the prompt body contains the literal `PROMPT`.) Codex returns plain text on stdout; expect the same JSON shape the prompt requests. If the user has a preferred Codex model configured in `~/.codex/config.toml`, that wins; otherwise default model.

**If `codex` is missing on the host** (`command -v codex` returns empty), skip Reviewer B, run only Reviewer A, and set `codex_skipped: true` in the response.

Important: don't pass the source via tool calls / file reads from either reviewer's perspective. The source MUST be inline in the prompt body so each reviewer reads it as part of the audit, not separately. For very large source content (>100KB), warn the caller — the prompts will be expensive but should still work.

### Phase 4 — Parse and merge the verdicts

Each reviewer returns the JSON shape from the Phase 2 scaffold. Two failure modes per reviewer:
- **Malformed JSON:** retry once with the explicit reminder appended ("return ONLY the JSON object, no surrounding prose, no markdown code fences"). If still malformed, treat that reviewer as `parse_error` and continue with the other.
- **Missing required fields** (no `summary`, no `verdicts`, verdicts missing `draft_id`/`verdict`/`issues`): same retry-once policy.

If BOTH reviewers `parse_error`, return the `parse_error` shape (Phase 5). If only ONE parses, proceed with that reviewer's verdicts and set `codex_parse_error: true` (or `claude_parse_error: true`) in the response.

**Merge rule (when both reviewers returned valid verdicts):**
- For each `draft_id`, the merged verdict is FAIL if EITHER reviewer marked it FAIL; PASS only if BOTH marked it PASS.
- The merged `issues` array is the deduplicated union of both reviewers' issues for that draft. Prefix each issue with its source: `[claude] ...` or `[codex] ...`. Issues that both reviewers raise (substring overlap of ≥10 chars or ≥80% Jaccard on tokens) collapse to a single `[both] ...` entry.
- The merged `summary` is `all_pass` if every merged verdict is PASS, otherwise `some_fail`.

Why merge this way (FAIL-OR rather than majority-vote): a single reviewer catching a real fabrication is more valuable than a second reviewer missing it. False positives are cheap (the composer revises and re-submits); false negatives are expensive (a fabricated quote ships).

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
      "[both] Contains 'every leader I respect keeps a token from a past reskilling on their desk' — unverifiable third-party claim, source doesn't make this claim",
      "[codex] Stat '73% of teams adopt within 6 weeks' is not present in the source"
    ]}
  ],
  "reviewers": ["claude", "codex"]
}
```

The `reviewers` field surfaces who actually contributed (drops `codex` if `codex_skipped: true`, drops `claude` only in the unusual case the Agent tool failed). Each issue is prefixed `[claude]`, `[codex]`, or `[both]` so the caller can see which reviewer flagged what.

**Hard-fail shape (input validation OR both reviewers malformed):**

```json
{
  "summary": "parse_error",
  "verdicts": [],
  "reviewers": [],
  "error": "rules_list is empty",
  "raw_response": "<raw text from reviewer(s) if Phase 4 failed; otherwise empty>"
}
```

**Degraded shape (codex unavailable on host):**

```json
{
  "summary": "all_pass",
  "verdicts": [...],
  "reviewers": ["claude"],
  "codex_skipped": true,
  "codex_skip_reason": "codex CLI not on PATH"
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
