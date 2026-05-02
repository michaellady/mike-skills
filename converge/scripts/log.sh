#!/usr/bin/env bash
# CONVERGE LOG / REVIEW.md writer.
#
# Subcommands:
#   init <log-file>              Ensure the log has the standard header and a
#                                dated `### Run YYYY-MM-DD HH:MM` subsection.
#   row  <log-file> <round> <author> <verdict> <issues> <concessions>
#                                Append one row to the table.
#   smoke <log-file> <result>    Append a `Smoke check: <result>` line.
#   note  <log-file> <text>      Append free-form text (e.g. deadlock notes).
#
# All subcommands create the file if missing.
set -euo pipefail

cmd="${1:-}"; shift || true
[ -z "$cmd" ] && { echo "usage: $0 init|row|smoke|note ..." >&2; exit 2; }

case "$cmd" in
  init)
    file="${1:?log file path required}"
    mkdir -p "$(dirname "$file")"
    if [ ! -f "$file" ] || ! grep -q '^## CONVERGE LOG' "$file" 2>/dev/null; then
      cat >> "$file" <<'EOF'

## CONVERGE LOG

| Round | Author | Verdict | Issues raised | Issues conceded |
|-------|--------|---------|---------------|-----------------|
EOF
    fi
    printf '\n### Run %s\n\n' "$(date '+%Y-%m-%d %H:%M')" >> "$file"
    ;;
  row)
    file="${1:?}"; round="${2:?}"; author="${3:?}"; verdict="${4:?}"; issues="${5:-}"; conceded="${6:-}"
    printf '| %s | %s | %s | %s | %s |\n' \
      "$round" "$author" "$verdict" "${issues:-(none)}" "${conceded:-(none)}" >> "$file"
    ;;
  smoke)
    file="${1:?}"; result="${2:?}"
    printf 'Smoke check: %s\n' "$result" >> "$file"
    ;;
  note)
    file="${1:?}"; shift
    printf '%s\n' "$*" >> "$file"
    ;;
  *)
    echo "log.sh: unknown subcommand $cmd" >&2; exit 2 ;;
esac
