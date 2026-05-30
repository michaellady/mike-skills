---
name: ladder-the-failure
description: Use when a deployed/wired integration is FAILING (silently or opaquely) and you're tempted to try a fix. The failure lives at one of several layers — your wiring, your env, your IAM, an org policy/SCP, an account-level enablement/subscription, or the provider's response format — and each layer has a DIFFERENT OWNER. Ladder down them, verify what you own is actually applied, and get to the ONE authoritative signal (the real log line, an elevated-creds probe, latency-as-discriminator) before attributing blame or attempting a fix. Triggers — "why is this silently failing", "it deployed but doesn't work", "ladder the failure", "localize before fixing", "read the real error", "is it my code or their config", "diagnose the integration", "AccessDenied but the IAM looks right".
user_invocable: true
---

# ladder-the-failure

A deployed integration that silently fails is **not one bug** — it's a bug at one of several stacked layers, and the layers have **different owners**. "Enrichment comes back empty" could be your code (nil client), your config (env not applied), your IAM (role lacks the action), an **org policy** (an SCP that denies even your account's root), an **account-level gate** (model access, a marketplace subscription, a EULA, a quota), or the **provider's response format** (a quirk your client mishandles). Guessing among them is expensive twice over: each blind fix is a deploy cycle, and "fixing" the **wrong** layer (broadening IAM when the real block is an SCP) adds risk and noise while the real cause survives.

The discipline: **don't iterate fixes on inference. Ladder down the ownership stack, confirm the layers you own are actually applied, and get to the single authoritative signal that names the failing layer — then fix once, at the right layer.** This is the debugging counterpart to `probe-before-wire`: that one smokes a dependency *before* wiring; this one diagnoses a dependency that's *already wired and failing*.

## When to use

A thing you deployed/enabled doesn't work and the failure is opaque: a silent empty result, a generic 5xx, a "best-effort" path that quietly degraded, an `AccessDenied` that doesn't match the IAM you can see. Especially when you can list **two or more plausible causes with different owners** ("maybe model access, maybe IAM, maybe the VPC, maybe my request body") — that's the signal to localize instead of guess.

Do NOT use for a failure with one obvious cause and a stack trace pointing at it — just fix that.

## The ownership ladder

Walk it top-to-bottom. The ones you OWN come first because they're the cheapest to confirm and the most common real cause — verify each is *actually applied*, not just authored, before blaming anything external.

| Layer | Owner | "Actually applied?" check (read, don't assume) |
|---|---|---|
| **1. App wiring** | you | Does the code path construct + call the thing? (a `nil` client, an unwired handler, a build tag that excludes it) Grep the call site. |
| **2. Config / env** | you | Is the value present **at runtime**? Read the **deploy log** / the running env / the rendered template — NOT the source that was supposed to set it. |
| **3. Identity / role** | you | Does the caller's role grant the action on the resource? Read the attached policy, not the policy doc you wrote. |
| **4. Guardrails** | you/org | A **permissions boundary** or **org SCP** can deny even a correct role grant — and an SCP denies the account **root** too. If root is blocked, it's here. |
| **5. Account / tenant enablement** | the human / provider | Model access, a **marketplace subscription**, a EULA, a first-use form, a quota. Often must be done in a **management/billing account**, not the member account. |
| **6. Provider response format** | you (to absorb) | The live service returns a shape/quirk your client mishandles (fences around JSON, an envelope, a pagination wrapper) — invisible to fakes; see `maximize-verification`. |

## The three authoritative signals (use these instead of inferring)

1. **The real error / log line.** The single highest-value move. Get the creds, read the logs (`aws logs filter-log-events`, the platform's log view). Don't iterate fixes against a guessed error class. An **evolving** error tells a story: a `404 "use-case details not submitted"` that becomes a `403 "Subscribe not authorized"` means layer 5a cleared and layer 5b is now the wall.
2. **Latency as a discriminator.** Read the round-trip time before you read anything else. A **fast** failure (sub-second) is a *rejection* — permission / auth / validation, decided before any real work. A **slow** failure (near the client/integration timeout) is *network/route* — no path to the endpoint (missing NAT/VPC endpoint), a hung dependency. One number rules out a whole layer: if the client timeout is 15s and it fails in 0.5s, it is **not** a network timeout.
3. **An elevated-creds probe.** Invoke the dependency directly as an admin/root principal to bisect ownership. If **root is also denied** the same action → it's an **org SCP** (layer 4), not your role (layer 3) — you can't fix it from the member account. If **root succeeds but the service role fails** → it's the role grant or a subscription that hasn't propagated to that role. (Probing as root that *completes a subscription/EULA* accepts terms — get explicit user sign-off first; see the safety note below.)

## The procedure

1. **Capture the symptom precisely** — the exact response (status + the empty/wrong field) **and the latency**. "201 with `suggested_cleaner_copy` empty, ~0.5s" is a diagnosis-grade symptom; "it doesn't work" is not.
2. **Confirm your owned layers are APPLIED (1–4), reading ground truth** — the deploy log shows the env var set, the IAM policy created, the boundary permits the action, the call site is wired. Most "external service" failures die here, in your own plumbing.
3. **Read the authoritative error** (signal 1) — creds + logs. If you don't have creds, that's the blocker to clear next, not a reason to start guessing fixes.
4. **Localize with latency + an elevated-creds probe** (signals 2–3) before touching anything — name the failing layer.
5. **Attribute to the owning layer → that decides who fixes it and how.** Yours (wiring/env/IAM/code) → fix it. Org/account (SCP / subscription / use-case form / quota) → it's a human action, often in another account; hand them the *exact* grant needed, don't broaden your own IAM hoping it helps.
6. **Fix once, at that layer. If it was a live-only provider-format bug (layer 6), the fix needs a regression test that reproduces the live behavior with a fake** — because the fake passed before, so without it the bug silently returns.

## Safety note

Reading logs, checking IAM, and a *read-only* probe are fine to do autonomously. But an elevated-creds action that **accepts terms, completes a marketplace subscription, grants a permission, or changes an org policy** is on the "explicit user permission" list — surface what you found and get a yes first. Localizing the failure is yours; authorizing the org/account change is the user's.

## Worked example (the canonical case)

Symptom: the public promo linter returned `201` + a valid deterministic result but `suggested_cleaner_copy` was **empty**, round-trip **~0.5s**.

- **Owned layers, confirmed applied (read ground truth):** the deploy log showed `LORE_BEDROCK_MODEL` added to the Lambda env and the `InvokeModel` IAM policy *created*; the permissions boundary's deny-list didn't include Bedrock (`AllowEverythingElse` covered it); the call site (`buildWebHandler → newBedrockAnalyzer`) was wired on the Lambda path. So the analyzer was non-nil and InvokeModel was being attempted — not a wiring/env/IAM/boundary fault.
- **Latency ruled out a layer:** 0.5s ≪ the 15s client timeout → a **fast rejection**, not a VPC/network timeout. The "maybe the VPC has no route to Bedrock" hypothesis was killed by reading one number — no creds needed.
- **The authoritative error (creds + CloudWatch) named it, and it evolved:** `404 ResourceNotFoundException: use case details have not been submitted` → then `403 AccessDeniedException: not authorized for aws-marketplace:ViewSubscriptions/Subscribe`. The use-case gate had cleared; a marketplace subscription was now the wall.
- **An elevated-creds probe bisected ownership:** invoking as the dev-account **root** was *still* denied the marketplace action → only an **org SCP** can deny root → localized to the **management account**, not any role IAM I could fix. (I did **not** broaden the Lambda's IAM — that would've been the wrong-layer fix.)
- The human subscribed from the management account. The error then **changed again** to a fast decode failure: the live model (Claude Haiku 4.5) wrapped its JSON in a markdown code fence — a bug **the fake-transport tests never showed**. That was mine (layer 6): a defensive JSON-extraction helper, shipped with a regression test that reproduces the fence through a fake.

Every step **localized before acting.** No blind IAM broadening, no wrong-layer fix, no guess-and-deploy loop — the cost was reading a few logs and running two probes.

## Relationship to other skills

- **probe-before-wire** — the before/after pair. probe-before-wire smokes a dependency *before* baking its config (catching dead ids / permission gaps up front); ladder-the-failure diagnoses one that's *already wired and failing*. Both treat the real dependency as the oracle.
- **verified-ship** — once you've root-caused and the fix is yours, it ships through verified-ship's read-gated pipeline (and a layer-6 live-format fix's regression test is part of that gate).
- **maximize-verification** — layer 6 is its lesson made concrete: a faked external boundary is a correlated-failure trap, so a live smoke against the real dependency is a distinct, necessary independent check.
