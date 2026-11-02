#!/usr/bin/env bash
# demo.sh starts a throwaway ACC daemon filled with made-up sessions, for
# screenshots and trying the dashboards. Nothing touches ~/.acc or ~/.claude.
#
#   scripts/demo.sh [acc-binary]      # prints the dashboard URL; Ctrl-C stops it
#
# Port: ACC_DEMO_PORT (default 7399).
set -euo pipefail

ACC=${1:-acc}
PORT=${ACC_DEMO_PORT:-7399}
DIR=$(mktemp -d -t acc-demo)
export ACC_HOME=$DIR/acc ACC_SERVER=http://127.0.0.1:$PORT CLAUDE_CONFIG_DIR=$DIR/claude
mkdir -p "$ACC_HOME" "$CLAUDE_CONFIG_DIR/projects/demo"

# Its own HOME and data dir, so History shows only the made-up sessions.
mkdir -p "$DIR/home"
HOME=$DIR/home XDG_DATA_HOME=$DIR/data "$ACC" serve --addr "127.0.0.1:$PORT" >"$DIR/serve.log" 2>&1 &
SERVE=$!
trap 'kill $SERVE 2>/dev/null; rm -rf "$DIR"' EXIT
trap 'exit 1' INT TERM # run the cleanup when stopped, too
for _ in $(seq 50); do curl -s -o /dev/null "$ACC_SERVER/api/v1/stream" -m 0.2 && break; sleep 0.1; done
TOKEN=$(cat "$ACC_HOME/token")
api() { curl -s -o /dev/null -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }

now=$(date +%s)
iso() { date -u -r "$1" +%Y-%m-%dT%H:%M:%S.000Z 2>/dev/null || date -u -d "@$1" +%Y-%m-%dT%H:%M:%S.000Z; }

# Claude Code sessions: a transcript with usage (and maybe a goal), then hooks.
claude() { # id cwd title cost in out cacheRead added removed [goal]
  local t=$CLAUDE_CONFIG_DIR/projects/demo/$1.jsonl id=$1 cwd=$2 title=$3
  shift 3
  printf '{"type":"user","cwd":"%s","timestamp":"%s","message":{"content":"%s"}}\n' "$cwd" "$(iso $((now - 3600)))" "$title" >"$t"
  printf '{"type":"ai-title","aiTitle":"%s"}\n' "$title" >>"$t"
  printf '{"type":"cost-state","totalCostUSD":%s,"totalLinesAdded":%s,"totalLinesRemoved":%s,"modelUsage":{"claude-opus-5-5":{"inputTokens":%s,"outputTokens":%s,"cacheReadInputTokens":%s,"cacheCreationInputTokens":0,"costUSD":%s}}}\n' \
    "$1" "$5" "$6" "$2" "$3" "$4" "$1" >>"$t"
  if [ -n "${7:-}" ]; then
    printf '{"type":"attachment","timestamp":"%s","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"%s"}}\n' "$(iso $((now - 900)))" "$7" >>"$t"
    printf '{"type":"attachment","timestamp":"%s","attachment":{"type":"goal_status","met":false,"condition":"%s","reason":"2 tests in internal/store still fail","iterations":2}}\n' "$(iso $((now - 60)))" "$7" >>"$t"
  fi
  hook "$id" "\"hook_event_name\":\"SessionStart\",\"cwd\":\"$cwd\",\"transcript_path\":\"$t\",\"source\":\"startup\""
}
hook() { echo "{\"session_id\":\"$1\",$2}" | "$ACC" hook claude-code >/dev/null; }
status() { # id model ctx%
  echo "{\"session_id\":\"$1\",\"model\":{\"display_name\":\"$2\"},\"context_window\":{\"used_percentage\":$3,\"context_window_size\":200000},
    \"rate_limits\":{\"five_hour\":{\"used_percentage\":80,\"resets_at\":$((now + 100 * 60))},\"seven_day\":{\"used_percentage\":42,\"resets_at\":$((now + 3 * 86400))}}}" |
    "$ACC" statusline >/dev/null
}

API=7116efd3-730d-4c1e-9a52-3f0d2e8b6a10
WEB=9b2da2a2-c451-4e0b-8d7c-5a1f3c9e2b47
BILL=c3f81e20-5d4a-4b6f-a1e9-7e2d9b0c4f15
OLD=b667a6fd-1e2b-4c3d-8e4f-0a1b2c3d4e5f
DOCS=ses_19cb2a7e5ffeK2mDq8VzLwT3nP

claude $API /Users/me/code/api api-server 4.21 12400 53100 2400000 212 38 "all tests in ./internal pass"
hook $API '"hook_event_name":"UserPromptSubmit","prompt":"fix the flaky store test, then run the whole suite"'
hook $API '"hook_event_name":"SubagentStart","agent_id":"ad85f1","agent_type":"Explore"'
hook $API '"hook_event_name":"PreToolUse","agent_id":"ad85f1","agent_type":"Explore","tool_name":"Grep","tool_input":{"pattern":"retryLock"}'
hook $API '"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./internal/..."}'
hook $API '"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"go test ./internal/..."},"tool_response":{"stdout":"ok  \tgithub.com/acme/api/internal/http\t0.12s\n--- FAIL: TestRetryLock (0.04s)\nFAIL\tgithub.com/acme/api/internal/store\t0.31s"}'
status $API "Opus 5.5" 82

claude $WEB /Users/me/code/web web-ui 1.37 4100 18800 610000 96 41
hook $WEB '"hook_event_name":"UserPromptSubmit","prompt":"clean the build output and rebuild"'
hook $WEB '"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}'
status $WEB "Sonnet 5.5" 31

claude $BILL /Users/me/code/billing billing-webhooks 0.88 2300 9100 300000 24 6
hook $BILL '"hook_event_name":"UserPromptSubmit","prompt":"add an idempotency key to the invoice webhook"'
hook $BILL '"hook_event_name":"Stop","last_assistant_message":"Done: the webhook now stores the key and returns the first response for retries. Tests pass."'
status $BILL "Opus 5.5" 44

claude $OLD /Users/me/code/scratch scratch 0.12 300 1200 40000 0 0
hook $OLD '"hook_event_name":"SessionEnd","reason":"exit"'

# Sessions only in the agent's records, for History.
past() { # id cwd title age-in-days
  printf '{"type":"user","cwd":"%s","timestamp":"%s","message":{"content":"%s"}}\n' "$2" "$(iso $((now - $4 * 86400)))" "$3" \
    >"$CLAUDE_CONFIG_DIR/projects/demo/$1.jsonl"
  touch -t "$(date -r $((now - $4 * 86400)) +%Y%m%d%H%M.%S 2>/dev/null || date -d "@$((now - $4 * 86400))" +%Y%m%d%H%M.%S)" "$CLAUDE_CONFIG_DIR/projects/demo/$1.jsonl"
}
past 3f1c9a0e-77b2-4f0e-9c1d-2a6b8e4d1f03 /Users/me/code/ci "move the CI pipeline to the new runners" 2
past 8a2e4b19-0c3d-4e5f-a617-b28c9d0e1f24 /Users/me/code/api "add rate limiting to the public endpoints" 6

# An OpenCode session, as the ACC plugin reports it.
oc() { api -X POST "$ACC_SERVER/api/v1/agents/opencode/events" -d "$1"; }
oc "{\"type\":\"session.created\",\"location\":{\"directory\":\"/Users/me/code/docs\"},\"data\":{\"sessionID\":\"$DOCS\",\"location\":{\"directory\":\"/Users/me/code/docs\"}}}"
oc "{\"type\":\"session.renamed\",\"data\":{\"sessionID\":\"$DOCS\",\"title\":\"docs-site\"}}"
oc "{\"type\":\"session.step.started\",\"data\":{\"sessionID\":\"$DOCS\",\"model\":{\"id\":\"gpt-6.1\",\"providerID\":\"openai\"}}}"
oc "{\"type\":\"session.usage.updated\",\"data\":{\"sessionID\":\"$DOCS\",\"cost\":0.64,\"tokens\":{\"input\":8800,\"output\":6100,\"reasoning\":900,\"cache\":{\"read\":220000,\"write\":0}}}}"
oc "{\"type\":\"session.execution.succeeded\",\"acc_last_text\":\"Every page now links to the new API reference.\",\"data\":{\"sessionID\":\"$DOCS\"}}"
oc "{\"type\":\"rpc.opencode.goal.changed\",\"data\":{\"sessionID\":\"$DOCS\",\"goal\":{\"objective\":\"no broken links in the docs\",\"status\":\"complete\",\"reason\":\"the link checker reports 0 broken links\",\"rounds\":3,\"maxRounds\":50,\"updatedAt\":$((now * 1000))}}}"

# Groups and colours.
"$ACC" group set platform '#61afef' >/dev/null
"$ACC" group set platform/api >/dev/null
"$ACC" group set web '#c678dd' >/dev/null
"$ACC" group add platform/api $API >/dev/null
"$ACC" group add platform $BILL >/dev/null
"$ACC" group add web $WEB $DOCS >/dev/null
"$ACC" color $WEB '#e06c75' >/dev/null
"$ACC" send $BILL "also add a test for a retried webhook" >/dev/null
"$ACC" send ${API:0:8} "run the tests before committing" >/dev/null

# Plan history, so the pace forecast has something to work with: the 5-hour
# window went from 45% to 80% in the last 40 minutes.
sqlite3 "$ACC_HOME/acc.db" "INSERT INTO plan_samples (agent, key, ts, used_pct, resets_at)
  SELECT 'claude-code', 'five_hour', ($now - 40*60) * 1000, 45, resets_at FROM plan_samples WHERE key = 'five_hour' LIMIT 1;" 2>/dev/null || true
status $API "Opus 5.5" 82

# Keep the billing session's channel and the OpenCode plugin live, as if
# started with acc claude and running the ACC plugin.
live() { for s in $BILL $DOCS; do api -X POST "$ACC_SERVER/api/v1/sessions/$s/claim"; done; }
live
( while sleep 20; do live; done ) &

echo "ACC demo on $ACC_SERVER (ACC_HOME=$ACC_HOME)"
echo "  web: $ACC_SERVER/?token=$TOKEN"
echo "  tui: ACC_HOME=$ACC_HOME ACC_SERVER=$ACC_SERVER $ACC tui"
wait $SERVE
