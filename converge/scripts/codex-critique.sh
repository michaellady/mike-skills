#!/usr/bin/env bash
# Run codex exec with a prompt file, stream live status to stderr while the
# model runs, and emit only the final assistant message on stdout.
#
# Usage: codex-critique.sh <prompt-file> [<reasoning-effort>]
#   reasoning-effort defaults to "xhigh"; pass "high" / "medium" to lower it.
#
# Env:
#   CONVERGE_CODEX_TIMEOUT   per-call seconds (default 300)
#   CONVERGE_QUIET           if set/1, suppress the stderr heartbeat
#   CONVERGE_HEARTBEAT_S     min seconds between idle heartbeats (default 5)
#
# Stderr (live, prefixed with `[codex Ts]` where T is elapsed seconds):
#   [codex 0s]    starting (effort=xhigh, timeout=300s)
#   [codex 4s]    reasoning: <one-line summary>
#   [codex 11s]   tool_call: shell.exec ls -la
#   [codex 18s]   message: <one-line preview>
#   [codex 22s]   done (final message: 1834 chars)
#
# Exits:
#   0  success (final message on stdout)
#   2  missing prompt file or codex CLI
#   3  codex auth error
#   4  timeout
#   5  no final assistant message in stream
set -uo pipefail

prompt_file="${1:-}"
effort="${2:-xhigh}"
timeout_s="${CONVERGE_CODEX_TIMEOUT:-300}"
quiet="${CONVERGE_QUIET:-0}"
heartbeat_s="${CONVERGE_HEARTBEAT_S:-5}"

if [ -z "$prompt_file" ] || [ ! -f "$prompt_file" ]; then
  echo "codex-critique: prompt file not found: $prompt_file" >&2; exit 2
fi
if ! command -v codex >/dev/null 2>&1; then
  echo "codex-critique: codex CLI not on PATH (npm install -g @openai/codex)" >&2; exit 2
fi

if command -v gtimeout >/dev/null 2>&1; then TIMEOUT=gtimeout
elif command -v timeout >/dev/null 2>&1; then TIMEOUT=timeout
else TIMEOUT=""
fi

err=$(mktemp -t converge-codex-err.XXXXXX)
final_file=$(mktemp -t converge-codex-final.XXXXXX)
trap 'rm -f "$err" "$final_file"' EXIT

prompt=$(cat "$prompt_file")

stream_filter() {
  # stdin: codex JSONL. stdout: final assistant message (written at EOF).
  # stderr: live human-readable status events.
  PYTHONUNBUFFERED=1 python3 -u - "$quiet" "$heartbeat_s" "$final_file" <<'PY'
import sys, json, time, os
quiet = sys.argv[1] in ("1","true","yes")
heartbeat = max(1, int(sys.argv[2] or "5"))
final_path = sys.argv[3]
start = time.time()
last_log = start
last = None

def elapsed():
    return int(time.time() - start)

def log(msg):
    if quiet: return
    sys.stderr.write(f"[codex {elapsed()}s] {msg}\n")
    sys.stderr.flush()

def trim(s, n=80):
    s = " ".join((s or "").split())
    return s if len(s) <= n else s[: n - 1] + "…"

log(f"starting (effort={os.environ.get('CONVERGE_EFFORT','?')}, timeout={os.environ.get('CONVERGE_CODEX_TIMEOUT','300')}s)")

for line in sys.stdin:
    line = line.strip()
    if not line:
        if not quiet and time.time() - last_log >= heartbeat:
            log("…still running")
            last_log = time.time()
        continue
    try:
        obj = json.loads(line)
    except Exception:
        continue
    t = obj.get("type", "")
    item = obj.get("item", {}) if isinstance(obj.get("item"), dict) else {}
    it_type = item.get("item_type", "")

    if t == "thread.started":
        log(f"thread {obj.get('thread_id','?')[:8]} started")
    elif t == "turn.started":
        log("turn started")
    elif t in ("agent_reasoning","reasoning","item.completed") and (it_type == "reasoning" or t.endswith("reasoning")):
        text = item.get("text") or obj.get("text") or item.get("summary") or ""
        if text: log(f"reasoning: {trim(text)}")
    elif t in ("tool_call","item.started") and it_type in ("tool_call","command_execution"):
        name = item.get("tool") or item.get("name") or "tool"
        args = item.get("arguments") or item.get("command") or ""
        log(f"tool: {name} {trim(str(args), 60)}")
    elif t in ("agent_message","assistant_message","message") and "text" in obj:
        last = obj["text"]; log(f"message: {trim(last)}")
    elif t == "item.completed" and it_type == "assistant_message":
        last = item.get("text", last); log(f"message: {trim(last or '')}")
    elif t in ("turn.completed","thread.completed"):
        log("turn complete")

    last_log = time.time()

if last is None:
    log("ERROR: no final assistant message")
    sys.exit(5)
with open(final_path, "w") as f:
    f.write(last)
log(f"done (final message: {len(last)} chars)")
PY
}

export CONVERGE_EFFORT="$effort"

if [ -n "$TIMEOUT" ]; then
  "$TIMEOUT" "$timeout_s" codex exec --skip-git-repo-check "$prompt" \
    -s read-only -c "model_reasoning_effort=\"${effort}\"" \
    --enable web_search_cached --json < /dev/null 2>"$err" \
    | stream_filter
  rc_codex=${PIPESTATUS[0]}
  rc_filter=${PIPESTATUS[1]}
else
  codex exec --skip-git-repo-check "$prompt" \
    -s read-only -c "model_reasoning_effort=\"${effort}\"" \
    --enable web_search_cached --json < /dev/null 2>"$err" \
    | stream_filter
  rc_codex=${PIPESTATUS[0]}
  rc_filter=${PIPESTATUS[1]}
fi

if [ "$rc_codex" = "124" ]; then
  echo "codex-critique: timed out after ${timeout_s}s" >&2; exit 4
fi
if grep -qiE 'not authenticated|401|unauthor' "$err" 2>/dev/null; then
  echo "codex-critique: auth error — run \`codex login\`" >&2; exit 3
fi
if [ "$rc_filter" = "5" ] || [ ! -s "$final_file" ]; then
  echo "codex-critique: no final assistant message in JSONL stream" >&2
  if [ -s "$err" ]; then echo "stderr: $(head -c 500 "$err")" >&2; fi
  exit 5
fi

cat "$final_file"
