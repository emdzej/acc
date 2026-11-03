package codex

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// A rollout as Codex 0.166 writes it: session meta, turn context, token
// counts with rate limits.
const rollout = `{"timestamp":"2026-11-03T08:00:00.000Z","type":"session_meta","payload":{"id":"019a2b3c","timestamp":"2026-11-03T08:00:00.000Z","cwd":"/code/api","cli_version":"0.166.0"}}
{"timestamp":"2026-11-03T08:00:01.000Z","type":"turn_context","payload":{"cwd":"/code/api","model":"gpt-5.5-codex"}}
{"timestamp":"2026-11-03T08:00:05.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"model_context_window":272000},"rate_limits":null}}
{"timestamp":"2026-11-03T08:01:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5000,"cached_input_tokens":3000,"cache_write_input_tokens":100,"output_tokens":700,"reasoning_output_tokens":300,"total_tokens":5700},"last_token_usage":{"input_tokens":4000,"cached_input_tokens":2400,"output_tokens":500,"reasoning_output_tokens":250,"total_tokens":27200},"model_context_window":272000},"rate_limits":{"primary":{"used_percent":42.5,"window_minutes":300,"resets_at":1793710000},"secondary":{"used_percent":12,"window_minutes":10080,"resets_at":1794200000},"plan_type":"plus"}}}
{"timestamp":"2026-11-03T08:01:01.000Z","type":"event_msg","payload":{"type":"agent_message","message":"done"}}
not json
`

func TestRollout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "sessions", "2026", "11", "03", "rollout-2026-11-03T08-00-00-019a2b3c.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(rollout), 0o644)

	u, st, err := Rollout(path)
	if err != nil {
		t.Fatal(err)
	}
	// The latest running total wins; cached input is reported separately.
	if u.InputTokens != 2000 || u.CacheReadTokens != 3000 || u.CacheWriteTokens != 100 || u.OutputTokens != 700 || u.CostKnown || len(u.Models) != 1 {
		t.Errorf("usage: %+v", u)
	}
	if st.Model != "gpt-5.5-codex" || st.ContextPct == nil || *st.ContextPct != 10 || st.ContextSize != 272000 {
		t.Errorf("status: %+v", st)
	}
	if len(st.Plan) != 2 || st.Plan[0].Key != "five_hour" || st.Plan[0].Label != "5h" || st.Plan[0].UsedPct != 42.5 ||
		st.Plan[0].ResetsAt != 1793710000000 || st.Plan[1].Key != "seven_day" {
		t.Errorf("plan: %+v", st.Plan)
	}
	other := filepath.Join(t.TempDir(), "rollout.jsonl")
	os.WriteFile(other, []byte(rollout), 0o644)
	if _, _, err := Rollout(other); err != ErrForbidden {
		t.Errorf("outside Codex's sessions: %v", err)
	}
	if w := planWindow(window{UsedPercent: 1, WindowMinutes: ptr(int64(1440))}); w.Key != "1_day" || w.Label != "1d" {
		t.Errorf("1 day window: %+v", w)
	}
}

func ptr[T any](v T) *T { return &v }

// The threads table as in Codex 0.166's state_5.sqlite.
const threadsSchema = `CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    rollout_path TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    source TEXT NOT NULL,
    model_provider TEXT NOT NULL,
    cwd TEXT NOT NULL,
    title TEXT NOT NULL,
    sandbox_policy TEXT NOT NULL,
    approval_mode TEXT NOT NULL,
    tokens_used INTEGER NOT NULL DEFAULT 0,
    has_user_event INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    archived_at INTEGER,
    git_sha TEXT,
    git_branch TEXT,
    git_origin_url TEXT
, cli_version TEXT NOT NULL DEFAULT '', first_user_message TEXT NOT NULL DEFAULT '', agent_nickname TEXT, agent_role TEXT, memory_mode TEXT NOT NULL DEFAULT 'enabled', model TEXT, reasoning_effort TEXT, agent_path TEXT, created_at_ms INTEGER, updated_at_ms INTEGER, thread_source TEXT, preview TEXT NOT NULL DEFAULT '', recency_at INTEGER NOT NULL DEFAULT 0, recency_at_ms INTEGER NOT NULL DEFAULT 0, history_mode TEXT NOT NULL DEFAULT 'legacy', name TEXT, is_pinned INTEGER NOT NULL DEFAULT 0);`

func TestSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	for _, f := range []string{"state_4.sqlite", "state_5.sqlite"} {
		os.WriteFile(filepath.Join(home, f), nil, 0o644)
	}
	if got := StateDB(); filepath.Base(got) != "state_5.sqlite" {
		t.Fatalf("StateDB: %s", got)
	}
	path := filepath.Join(home, "state_5.sqlite")
	os.Remove(path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(threadsSchema); err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO threads (id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode,
  has_user_event, archived, first_user_message, name, agent_path, created_at_ms, updated_at_ms) VALUES (?, ?, 1, ?, 'cli', 'openai', ?, ?, 'x', 'x', ?, ?, ?, ?, ?, ?, ?)`
	for _, r := range [][]any{
		{"t-named", "/r/1.jsonl", 10, "/code/api", "auto title", 1, 0, "first prompt", "my name", nil, 1000, 10000},
		{"t-titled", "/r/2.jsonl", 20, "/code/web", "Fix the   flaky\ntests", 1, 0, "first", nil, nil, 2000, 20000},
		{"t-prompt", "/r/3.jsonl", 30, "/code/cli", "", 0, 0, "just the prompt", "", nil, nil, nil}, // as Codex 0.166 writes it
		{"t-empty", "/r/4.jsonl", 40, "/code/x", "", 0, 0, "", nil, nil, 4000, 40000},               // no prompt yet
		{"t-archived", "/r/5.jsonl", 50, "/code/x", "old", 1, 1, "", nil, nil, 5000, 50000},
		{"t-sub", "/r/6.jsonl", 60, "/code/x", "sub", 1, 0, "", nil, "/root/explorer", 6000, 60000},
	} {
		if _, err := db.Exec(ins, r...); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	got, err := Sessions(t.Context(), path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "t-prompt" || got[1].ID != "t-titled" || got[2].ID != "t-named" {
		t.Fatalf("sessions: %+v", got)
	}
	if got[0].Title != "just the prompt" || got[0].LastActive != 30000 || got[1].Title != "Fix the flaky tests" || got[2].Title != "my name" ||
		got[2].Cwd != "/code/api" || got[2].StartedAt != 1000 || got[2].TranscriptPath != "/r/1.jsonl" || got[2].Agent != Agent {
		t.Errorf("fields: %+v", got)
	}
	if s, err := Sessions(t.Context(), filepath.Join(home, "none.sqlite"), 10); err != nil || s != nil {
		t.Errorf("missing db: %v %v", s, err)
	}
}
