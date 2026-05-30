---
name: probe-before-wire
description: Use before baking an external config value — a model id, endpoint, ARN, bucket, region, credential, API version, image tag — into code or a deploy. Invoke the real dependency ONCE against the target account/environment and read the response, so a dead/renamed/deprecated/permission-blocked value is caught at the source instead of failing in production. The config equivalent of "verify against reality, not the doc." Triggers — "probe before wire", "smoke the dependency first", "is this model/endpoint/ARN actually live", "verify the config before deploy", "does this id still work", "check it against the real account".
user_invocable: true
---

# probe-before-wire

A config value copied from a doc, a PRD, a prior project, or your own memory is an **unverified claim about an external system you don't control.** Providers retire model ids, rename endpoints, deprecate API versions, rotate ARNs, and gate first-use behind a form — on their schedule, not yours. The doc that was right when it was written silently goes stale.

The discipline: **before a config value enters code or a deploy, invoke the real thing once against the target account/environment and read what comes back.** One live round-trip turns "this id should work" into "this id returned a 200 / a completion / the expected shape" — or surfaces the exact failure (Legacy, AccessDenied, NotFound, needs-EULA) while it's a one-line fix instead of a production incident.

This is `maximize-verification`'s "anchor on an oracle you didn't author — verify against reality, not the doc" principle, applied to **configuration and infrastructure** rather than code. The real dependency IS the oracle.

## When to use

Reach for it whenever you're about to hardcode or set, as a default or a deploy var, any of:
- a model / inference-profile id (LLM, embeddings, vision)
- an API endpoint, base URL, or **API version** string
- an ARN, bucket name, queue/topic, secret name, parameter path
- a region, account id, or cross-region profile prefix
- a container image tag / digest, a package version pin
- a credential or role you assume is granted

Especially when the value came from: a doc/PRD/README, another repo, your own training data or memory, or "the obvious current version."

## The procedure

1. **Identify the real invocation.** What single call exercises this value end-to-end against the *target* account/env? (`aws bedrock-runtime invoke-model`, a `curl` to the endpoint, `aws s3api head-bucket`, `aws sts get-caller-identity` for the credential, `gh api` for a repo setting, a `HEAD` for an image.)
2. **Confirm the identity/profile first.** Check which credential/profile is actually live (`aws sts get-caller-identity`) before blaming the value — an `InvalidClientTokenId` on the default profile is an auth problem, not a config problem. Use the right `AWS_PROFILE` / context for the target account.
3. **Use the exact shape the code will send.** Match the request body / headers / version the production client uses, so the probe proves the *real* path, not a simplified one. (For an LLM: the same `anthropic_version`, the same Messages body. For an endpoint: the same auth + content-type.)
4. **Invoke once and READ the response.** Not just the exit code — the body. A `200` with a wrong-shaped payload still fails downstream.
5. **Classify the result:**
   - **Clean success** (a completion / the expected object) → the value is live; wire it in, and cite "smoke-verified against `<account/env>`" in the commit body.
   - **A validation/format error** → good news: you reached the service; the *value* is fine, your *request shape* is off. Fix the shape, re-probe.
   - **Legacy / deprecated / retired** → the value is dead. Find the current one (`aws bedrock list-foundation-models --query "...status=='ACTIVE'"`, the provider's current-models endpoint, `list-inference-profiles`) and re-probe the replacement before wiring.
   - **AccessDenied / NotFound / needs-EULA / needs-use-case-form** → a permission/enablement gap. Now you know *exactly* what the human must grant, before a deploy fails opaquely.
6. **Wire the verified value.** Only the value that actually returned clean goes into the default / the deploy var. If the codebase has a stale default of the same kind, fix it in the same change (a dead default is a latent failure).

## Why this beats "just deploy and see"

A deploy cycle is minutes-to-hours and the failure is opaque (a cold-start log line, a 5xx, a silent fallback). A direct probe is seconds and the error message names the cause. Probing *before* wiring also separates the two questions that a failed deploy conflates: "is my plumbing right?" vs "is the external value live?" — you answer the second one in isolation, first.

## Worked example (the canonical case)

Enabling an LLM seam, the codebase's reference model id was `claude-sonnet-4-20250514` (from a months-old PRD). Instead of setting the deploy var and pushing:
- `aws sts get-caller-identity --profile dev-admin` → confirmed the right account.
- `aws bedrock-runtime invoke-model --model-id us.anthropic.claude-sonnet-4-20250514-v1:0 ...` → **`Access denied. This Model is marked by provider as Legacy`** — the reference id was dead.
- `aws bedrock list-foundation-models --query "...status=='ACTIVE'"` + `list-inference-profiles` → the active 4.5-generation ids.
- `invoke-model --model-id us.anthropic.claude-haiku-4-5-...` → a clean `pong` completion.

Result: the dead default got fixed in code in the same change, the deploy var was set to a *verified* id, and a production "why is enrichment silently empty" debugging session never happened. The cost was four CLI calls.

## Relationship to other skills

- **maximize-verification** — same "oracle you didn't author" principle; this is its config/infra projection.
- **verified-ship** — probe-before-wire is a pre-ship gate; the verified value then flows through verified-ship's read-gated pipeline, and "smoke-verified against `<env>`" is a fact you're then allowed to put in the PR body.
