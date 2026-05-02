#!/usr/bin/env bash
# Remove per-round critique payloads. Logs and REVIEW.md are deliverables, not cleaned.
set -euo pipefail
rm -f /tmp/converge-claude-r*.json /tmp/converge-codex-r*.json /tmp/converge-prompt-*.txt
