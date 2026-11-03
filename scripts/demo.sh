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
DIR=$(mktemp -d /tmp/acc-demo.XXXX) # short paths in screenshots
export ACC_HOME=$DIR/acc ACC_SERVER=http://127.0.0.1:$PORT CLAUDE_CONFIG_DIR=$DIR/claude
mkdir -p "$ACC_HOME" "$CLAUDE_CONFIG_DIR/projects/demo"
# The other agents' homes, for the daemon and the hooks alike.
export CODEX_HOME=$DIR/home/.codex COPILOT_HOME=$DIR/home/.copilot GEMINI_CLI_HOME=$DIR/home \
  PI_CODING_AGENT_DIR=$DIR/home/.pi/agent

# Its own HOME and data dir, so History shows only the made-up sessions.
mkdir -p "$DIR/home"
HOME=$DIR/home XDG_DATA_HOME=$DIR/data "$ACC" serve --addr "127.0.0.1:$PORT" >"$DIR/serve.log" 2>&1 &
SERVE=$!
LIVE=
trap 'kill $SERVE $LIVE 2>/dev/null; rm -rf "$DIR"' EXIT
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

# A Codex session: hooks, and a rollout with tokens and plan limits.
CODEX=019a2f4e-8b1c-7d3e-a5f6-4c2b9e8d7a10
rollout=$CODEX_HOME/sessions/2026/11/03/rollout-$CODEX.jsonl
mkdir -p "$(dirname "$rollout")"
cat >"$rollout" <<JSON
{"type":"turn_context","payload":{"model":"gpt-5.5"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":412000,"cached_input_tokens":380000,"output_tokens":9400,"total_tokens":421400},"last_token_usage":{"total_tokens":118000},"model_context_window":272000},"rate_limits":{"primary":{"used_percent":37,"window_minutes":300,"resets_at":$((now + 3 * 3600))},"secondary":{"used_percent":12,"window_minutes":10080,"resets_at":$((now + 5 * 86400))}}}}
JSON
cx() { echo "{\"session_id\":\"$CODEX\",\"transcript_path\":\"$rollout\",\"cwd\":\"/Users/me/code/search\",$1}" | "$ACC" hook codex >/dev/null; }
cx '"hook_event_name":"SessionStart","source":"startup"'
cx '"hook_event_name":"UserPromptSubmit","prompt":"rebuild the search index with the new tokenizer"'
cx '"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"cargo run --release -- reindex"}'

# A pi session, as the ACC extension reports it, with its session file.
PI=7d3c2b1a-4e5f-4a6b-9c8d-0e1f2a3b4c5d
pifile=$PI_CODING_AGENT_DIR/sessions/--Users-me-code-cli--/2026-11-03_$PI.jsonl
mkdir -p "$(dirname "$pifile")"
cat >"$pifile" <<JSON
{"type":"session","id":"$PI","cwd":"/Users/me/code/cli","timestamp":"$(iso $((now - 2400)))"}
{"type":"message","message":{"role":"user","content":"add shell completions for zsh and fish"}}
{"type":"message","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Done."}],"usage":{"input":5200,"output":3100,"cacheRead":140000,"cacheWrite":9000,"cost":{"total":0.71}}}}
JSON
pi() { api -X POST "$ACC_SERVER/api/v1/agents/pi/events" -d "{\"ts\":$((now * 1000)),\"session\":{\"id\":\"$PI\",\"file\":\"$pifile\",\"cwd\":\"/Users/me/code/cli\",\"name\":\"cli-completions\"},\"model\":\"claude-opus-5-5\",$1}"; }
pi '"type":"session_start","data":{"reason":"startup"}'
pi '"type":"input","data":{"text":"add shell completions for zsh and fish"}'
pi '"type":"agent_settled","context":{"tokens":61000,"contextWindow":200000,"percent":30.5},"data":{"lastText":"Completions for zsh and fish are in completions/, wired into make install."}'

# A Copilot CLI session: hooks, its event log and workspace.
COPILOT=5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9
cpdir=$COPILOT_HOME/session-state/$COPILOT
mkdir -p "$cpdir"
cat >"$cpdir/events.jsonl" <<JSON
{"type":"session.start","data":{"sessionId":"$COPILOT","selectedModel":"gpt-6.1"}}
{"type":"session.usage_record","data":{"usage":{"model":"gpt-6.1","inputTokens":38000,"outputTokens":2900,"cacheReadTokens":120000,"cacheWriteTokens":0,"cost":0}}}
{"type":"assistant.message","data":{"content":"The release notes are drafted in CHANGELOG.md. Want me to open the PR?"}}
JSON
printf 'id: %s\ncwd: /Users/me/code/mobile\nname: release-notes\ncreated_at: %s\nupdated_at: %s\n' "$COPILOT" "$(iso $((now - 1800)))" "$(iso $((now - 120)))" >"$cpdir/workspace.yaml"
cpl() { echo "{\"session_id\":\"$COPILOT\",\"cwd\":\"/Users/me/code/mobile\",$1}" | "$ACC" hook copilot >/dev/null; }
cpl '"hook_event_name":"SessionStart","source":"new"'
cpl '"hook_event_name":"UserPromptSubmit","prompt":"draft the release notes for 2.4"'
cpl "\"hook_event_name\":\"Stop\",\"transcript_path\":\"$cpdir/events.jsonl\",\"stop_reason\":\"end_turn\""

# A Gemini CLI session: hooks and its chat recording.
GEMINI=a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5e
gmdir=$GEMINI_CLI_HOME/.gemini/tmp/pipeline
mkdir -p "$gmdir/chats"
echo /Users/me/code/pipeline >"$gmdir/.project_root"
gmfile=$gmdir/chats/session-2026-11-03T09-12-a1b2c3d4.jsonl
cat >"$gmfile" <<JSON
{"sessionId":"$GEMINI","startTime":"$(iso $((now - 1500)))","kind":"main"}
{"id":"u1","type":"user","content":[{"text":"backfill last month's events into the warehouse"}]}
{"id":"g1","type":"gemini","content":"","tokens":{"input":64000,"output":1800,"cached":41000,"thoughts":900},"model":"gemini-3-pro"}
JSON
gm() { echo "{\"session_id\":\"$GEMINI\",\"transcript_path\":\"$gmfile\",\"cwd\":\"/Users/me/code/pipeline\",$1}" | "$ACC" hook gemini >/dev/null; }
gm '"hook_event_name":"SessionStart","source":"startup"'
gm '"hook_event_name":"BeforeAgent","prompt":"backfill last month'"'"'s events into the warehouse"'
gm '"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"python -m pipeline.backfill --month 2026-10"}'

# Past sessions of the other agents, for History.
mkdir -p "$COPILOT_HOME/session-state/6f7a8b9c-0d1e-4f2a-b3c4-d5e6f7a8b9c0"
printf 'id: 6f7a8b9c-0d1e-4f2a-b3c4-d5e6f7a8b9c0\ncwd: /Users/me/code/mobile\nname: fix the iOS push token refresh\ncreated_at: %s\nupdated_at: %s\n' "$(iso $((now - 3 * 86400)))" "$(iso $((now - 3 * 86400)))" \
  >"$COPILOT_HOME/session-state/6f7a8b9c-0d1e-4f2a-b3c4-d5e6f7a8b9c0/workspace.yaml"
cat >"$gmdir/chats/session-2026-10-30T14-02-b2c3d4e5.jsonl" <<JSON
{"sessionId":"b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e","startTime":"$(iso $((now - 4 * 86400)))","summary":"Profile the slow join in the nightly job","kind":"main"}
JSON
touch -t "$(date -r $((now - 4 * 86400)) +%Y%m%d%H%M.%S 2>/dev/null || date -d "@$((now - 4 * 86400))" +%Y%m%d%H%M.%S)" "$gmdir/chats/session-2026-10-30T14-02-b2c3d4e5.jsonl"
sqlite3 "$CODEX_HOME/state_5.sqlite" "CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT, cwd TEXT, name TEXT, title TEXT, first_user_message TEXT,
    created_at INTEGER, updated_at INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER, archived INTEGER DEFAULT 0, agent_path TEXT, agent_role TEXT);
  INSERT INTO threads VALUES ('$CODEX', '$rollout', '/Users/me/code/search', '', '', 'rebuild the search index with the new tokenizer', $((now - 2000)), $now, NULL, NULL, 0, NULL, NULL);
  INSERT INTO threads VALUES ('019a1b2c-3d4e-7f5a-b6c7-d8e9f0a1b2c3', '', '/Users/me/code/search', '', 'Tune BM25 weights for product titles', 'tune the ranking', $((now - 5 * 86400)), $((now - 5 * 86400)), NULL, NULL, 0, NULL, NULL);"

# Groups and colours.
"$ACC" group set platform '#56b6c2' >/dev/null
"$ACC" group set platform/api >/dev/null
"$ACC" group set web '#c678dd' >/dev/null
"$ACC" group add platform/api $API >/dev/null
"$ACC" group add platform $BILL >/dev/null
"$ACC" group add web $WEB $DOCS $COPILOT >/dev/null
"$ACC" group set data '#98c379' >/dev/null
"$ACC" group add data $GEMINI $CODEX >/dev/null
"$ACC" color $WEB '#e06c75' >/dev/null
"$ACC" rename $CODEX search-reindex >/dev/null
"$ACC" rename $COPILOT release-notes >/dev/null
"$ACC" rename $GEMINI events-backfill >/dev/null
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
LIVE=$!

echo "ACC demo on $ACC_SERVER (ACC_HOME=$ACC_HOME)"
echo "  web: $ACC_SERVER/?token=$TOKEN"
echo "  tui: ACC_HOME=$ACC_HOME ACC_SERVER=$ACC_SERVER $ACC tui"
wait $SERVE
