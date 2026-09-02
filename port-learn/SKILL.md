---
name: port-learn
description: Use when a port-* phase gate has just been passed or failed, or a port has ended, so the skill family improves itself from experience — file each finding, classify it as a rediscovery (the rule existed and did not reach the decision), a new rule, a tool gap, or target-local, and turn the first three into a reviewed change to the port-* skills with the finding cited and a canary showing the change would have fired. The standing phase of port-jvm-to-rust. Triggers — "what did we learn from this port", "file a finding", "update the porting skill", "rediscovery", "did we already know this", "compound the learnings from the port", "make the skill better".
user_invocable: true
---

# port-learn

The source projects behind this family recorded a specific failure: four of eighteen findings had **already been written down** — one of them thirteen hours earlier, verbatim, by the same owner on the same machine — and were rediscovered from scratch at full cost. *An insight that is not reachable at the moment of the decision costs the same as one never written.* Nine notes in a directory nobody opens mid-task is where rediscoveries come from; a rule inside the phase skill that the agent must read at the moment it does the work is not.

So this phase does two things. It files findings in a fixed shape so they are comparable across ports. And it **edits the port-* skills themselves** — not a notes directory — so the next port's agent meets the rule at the step where it applies. The highest-priority signal it tracks is the **rediscovery**: a finding that matches a rule already in the family. A rediscovery is never "we already knew that"; it is proof the rule was in the wrong place, and the fix is reachability, not another note.

Outputs: `.port/learn/F<nnn>-<slug>.md` per finding in the target; a reviewed PR against the skill repository for every rule change.

## When to Use

Use when:
- Any `port-*` phase gate is passed or failed — run this before invoking the next phase. Two minutes if nothing happened.
- A port ends (after `port-ceiling`) — the full pass.
- Someone says "we hit this before" — that sentence is a rediscovery until shown otherwise.
- A gate's canary failed to fail, a blocker turned out stale, a count decomposed, or a tool did something its documentation did not say.

Do NOT use when:
- The observation is target-specific and stays in the target's ledger (a `D-number` is not a finding).
- The proposed change has no finding behind it. The family does not grow from opinions; it grows from things that cost something.

## Inputs

- The phase just completed: its output file, its gate record, its canary record
- `port-jvm-to-rust/REFERENCE.md` (the checklists a finding is matched against) and every `port-*/SKILL.md`
- Prior findings: `.port/learn/` in this target, and the skill repository's `port-learn/FINDINGS.md` index across ports

## Workflow

### 1. Harvest, in a fixed shape

For anything that surprised, cost, or reversed a belief during the phase, one file:

```
# F<nnn> — <one line: the mechanism, not the symptom>
phase: port-<name>   step: <n>   date: <utc>
what happened: <two sentences, from tool output not memory>
what it cost: <time / a wrong number quoted / a gate that could not fail>
where the deciding moment was: <the exact step of the exact skill where a rule would have fired>
evidence: <paths, commands, the passing exit that should have been a refusal>
```

Benign findings count. Findings that correct *this port's own earlier claims* are kept in place with the wrong number in a collapsed block — how it was wrong is more useful than the finding.

### 2. Classify — the four bins

Match the finding against every checklist in REFERENCE (§ 3 counts, § 4 polarity forms and canary table, § 5 impedance, § 6 nondeterminism classes) and every step of every `port-*` skill.

| bin | test | action |
|---|---|---|
| **REDISCOVERY** | a rule already states this | the rule is in the wrong place; move or duplicate it *into the step* named in "where the deciding moment was", as an imperative the agent cannot read past. Log it in `FINDINGS.md` with the rule it matched and the distance (which file, how many steps away) |
| **NEW RULE** | no rule states it, and it will recur on another JVM target | add it to the checklist it belongs to *and* to the step where it fires; both, never just the checklist |
| **TOOL GAP** | the rig or a tool could not do what the phase needed (a canary with no runner, a verdict read from an exit code, a manifest that needed a code change) | a story against the rig; until it lands, a "known gap" line in the affected skill's gate |
| **TARGET-LOCAL** | true of this original only | stays in `.port/`, no skill change |

Two findings in the same bin from two different ports is the threshold for promoting a rule from a phase skill into REFERENCE. A rule cited by no finding in three ports is a candidate for pruning — **shortening the checklists is also learning**; a checklist nobody can hold is a directory nobody opens.

### 3. Make the change, with a canary

Every skill edit is a PR against the skill repository that:
- cites the finding file(s) by id in the commit message and in a `<!-- F<nnn> -->` comment beside the rule
- states the deciding moment it now fires at
- includes a **would-have-fired check**: re-read the finding's "what happened" against the edited skill and confirm, in the PR description, the sentence in the skill that now prevents it — quote it. A rule change that cannot point at the sentence that would have stopped the cost is a note, not a rule
- for a rediscovery, records the *distance* the rule moved (from REFERENCE § 4 to `port-capture` step 6, say) so the pattern of where rules fail to reach is itself measurable over time

Hand the PR to `converge audit` with the rules: "the change is justified by the cited finding", "the rule is placed at the step where the decision is made, not in a reference section", "no existing rule was weakened", "the would-have-fired sentence is quoted".

### 4. Update the index

`port-learn/FINDINGS.md` in the skill repository: one row per finding across all ports — id, target, phase, bin, rule matched or added, distance. The rediscovery *rate* per port is the family's health metric: it should fall. If it does not, the rules are accumulating in the wrong places and step 2's pruning is overdue.

### 5. The end-of-port pass

After `port-ceiling`: re-run steps 1–4 over the whole port with the ceiling report in hand, and additionally ask each of these once:

- which gate's canary was the last to be added, and what did the gate pass before it existed?
- which count was quoted before its four questions were asked?
- which blocker was cited without being re-run?
- which agent-written comparator was reviewed only by the agent that wrote it?
- what would a different model have found? — run `converge audit` over `.port/learn/` with "find the finding these findings imply and did not state"
- what did this port cost per KLOC, and which phase dominated? — that number goes to `port-qualify`'s estimator

## Gate

Every finding filed in the fixed shape; every non-target-local finding has a PR with a quoted would-have-fired sentence and a cross-model audit; the index is updated; the rediscovery rate is recorded. **Canary:** take one existing rule, delete it from its skill in a scratch copy, re-run step 2 against a past finding that rule matched — the classifier must report REDISCOVERY, not NEW RULE. A learning phase that cannot tell "we knew this" from "this is new" will file the same rule forever.

## Common Mistakes

- **Writing to a notes directory.** That is the failure this phase exists to end. The change goes into the step.
- **Filing opinions.** No finding, no change.
- **Adding without pruning.** The checklists' length is a cost paid at every decision.
- **Letting the implementing agent review its own rule change.** Same correlated-failure trap, one level up.
- **Treating a rediscovery as embarrassment instead of data.** It is the most valuable finding the phase produces: it says exactly where the family's rules fail to reach.
