#!/usr/bin/env bash
# Validate a converge critique JSON file matches the expected schema.
# Usage: validate-critique.sh <json-file>
# Exits 0 on valid, 1 with stderr message on invalid.
# Required fields: round (int), author (string), mode (string), verdict (one of
# needs_revision|converged), issues (array). Optional: concessions, open_disagreements.
# Each issue must have: id, severity (critical|major|minor), claim, rationale, proposed_fix.
# evidence is required when CONVERGE_REQUIRE_EVIDENCE=1 (set by caller for
# implement/verify/review modes).
set -euo pipefail

file="${1:-}"
if [ -z "$file" ] || [ ! -f "$file" ]; then
  echo "validate-critique: file not found: $file" >&2; exit 1
fi

require_evidence="${CONVERGE_REQUIRE_EVIDENCE:-0}"

python3 - "$file" "$require_evidence" <<'PY'
import sys, json
path, req_ev = sys.argv[1], sys.argv[2] == "1"
errs = []
try:
    with open(path) as f:
        d = json.load(f)
except Exception as e:
    print(f"invalid JSON: {e}", file=sys.stderr); sys.exit(1)

def need(obj, key, types, ctx="root"):
    if key not in obj:
        errs.append(f"{ctx}: missing '{key}'"); return None
    if not isinstance(obj[key], types):
        errs.append(f"{ctx}: '{key}' wrong type"); return None
    return obj[key]

if not isinstance(d, dict):
    print("top-level must be object", file=sys.stderr); sys.exit(1)
need(d, "round", int)
need(d, "author", str)
need(d, "mode", str)
v = need(d, "verdict", str)
if v not in (None, "needs_revision", "converged"):
    errs.append(f"verdict must be needs_revision|converged, got {v!r}")
issues = need(d, "issues", list)
if issues is not None:
    for i, it in enumerate(issues):
        ctx = f"issues[{i}]"
        if not isinstance(it, dict): errs.append(f"{ctx}: not an object"); continue
        for k in ("id","claim","rationale","proposed_fix"): need(it, k, str, ctx)
        sev = need(it, "severity", str, ctx)
        if sev not in (None, "critical","major","minor"):
            errs.append(f"{ctx}: severity must be critical|major|minor")
        if req_ev:
            ev = it.get("evidence")
            if not isinstance(ev, str) or not ev.strip():
                errs.append(f"{ctx}: 'evidence' required for this mode")

for opt in ("concessions","open_disagreements"):
    if opt in d and not isinstance(d[opt], list):
        errs.append(f"{opt}: must be array if present")

if errs:
    for e in errs: print(e, file=sys.stderr)
    sys.exit(1)
PY
