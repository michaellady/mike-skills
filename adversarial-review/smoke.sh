#!/usr/bin/env bash
# End-to-end smoke test for the adversarial-review binary against every
# registered provider. Sends a tiny prompt and asserts each provider returns
# a usable JSON verdict.
#
# Pure-logic Go tests (`go test ./...`) cover parse + merge + dedup but
# DO NOT exercise the actual `claude.Run()` / `codex.Run()` / `agent.Run()`
# / `agy.Run()` CLI dispatch. This script does.
#
# Run before committing any change to provider code or after upgrading a
# provider CLI. Burns a few cents of provider tokens per invocation.
#
# Usage:
#   _shared/adversarial-review/smoke.sh              # smoke every registered provider individually + N-way
#   _shared/adversarial-review/smoke.sh claude       # smoke just one provider
#   _shared/adversarial-review/smoke.sh claude codex # smoke a specific N-way combo
set -euo pipefail

dir=$(cd "$(dirname "$0")" && pwd)
bin="$dir/adversarial-review"
prompt=$(mktemp -t ar-smoke-prompt)
trap 'rm -f "$prompt"' EXIT

cat > "$prompt" <<'PROMPT'
You are an adversarial reviewer for a smoke test. Return ONLY this exact JSON object, no surrounding prose, no markdown code fences:

{"summary":"all_pass","verdicts":[{"draft_id":"smoke","verdict":"PASS","issues":[]}]}
PROMPT

if [[ ! -x "$bin" ]]; then
  echo "error: $bin not built. Run: cd $dir && go build ." >&2
  exit 2
fi

# All registered reviewers per main.go. Keep in sync if more are added.
all_reviewers=(claude codex agent agy)

# If args given, smoke just those. Otherwise smoke each individually + N-way.
if [[ $# -gt 0 ]]; then
  combos=("$*")
else
  combos=()
  for r in "${all_reviewers[@]}"; do combos+=("$r"); done
  IFS=, joined="${all_reviewers[*]}"
  combos+=("$joined")
fi

pass=0
fail=0
for combo in "${combos[@]}"; do
  # Convert space-separated to CSV
  csv=$(echo "$combo" | tr ' ' ',')
  echo "===== smoke: --reviewers $csv ====="
  out=$("$bin" --prompt-file "$prompt" --reviewers "$csv" --timeout 120 --quiet 2>/dev/null) || {
    echo "  FAIL: binary exited non-zero" >&2
    fail=$((fail + 1))
    continue
  }
  summary=$(echo "$out" | grep -o '"summary": *"[^"]*"' | head -1 | sed 's/.*: *"\([^"]*\)".*/\1/')
  if [[ "$summary" != "all_pass" ]]; then
    echo "  FAIL: summary=$summary" >&2
    echo "$out" | head -20 >&2
    fail=$((fail + 1))
  else
    reviewers=$(echo "$out" | tr -d '\n' | grep -o '"reviewers":[^]]*]' | head -1)
    echo "  OK: $reviewers"
    pass=$((pass + 1))
  fi
done

echo "---"
echo "smoke: $pass pass / $fail fail"
[[ $fail -eq 0 ]]
