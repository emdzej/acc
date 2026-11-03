package copilot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const id = "55555555-2222-4333-8444-555555555555"

// Lines as Copilot CLI 1.0.105 writes them (trimmed).
const events = `{"type":"session.start","data":{"sessionId":"` + id + `","copilotVersion":"1.0.105","selectedModel":"fake-1","context":{"cwd":"/code/work"}},"id":"8cee","timestamp":"2026-11-03T08:25:35.185Z"}
{"type":"session.usage_record","data":{"usage":{"model":"fake-1","inputTokens":120,"outputTokens":8,"cacheReadTokens":0,"cacheWriteTokens":0,"cost":0.0,"isByok":true}}}
{"type":"session.model_change","data":{"newModel":"claude-sonnet-4.5"}}
{"type":"session.usage_record","data":{"usage":{"model":"claude-sonnet-4.5","inputTokens":200,"outputTokens":30,"cacheReadTokens":50,"cacheWriteTokens":10,"cost":1}}}
`

const workspace = `id: ` + id + `
cwd: /code/work
client_name: github/cli
name: 'it''s a test'
user_named: false
created_at: 2026-11-03T08:25:35.130Z
updated_at: 2026-11-03T08:25:35.290Z
`

func write(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("COPILOT_HOME", home)
	os.MkdirAll(SessionDir(id), 0o755)
	os.WriteFile(EventsPath(id), []byte(events), 0o644)
	path := filepath.Join(SessionDir(id), "workspace.yaml")
	os.WriteFile(path, []byte(workspace), 0o644)
	return path
}

func TestUsage(t *testing.T) {
	write(t)
	u, st, err := Usage(EventsPath(id))
	if err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != 320 || u.OutputTokens != 38 || u.CacheReadTokens != 50 || u.CacheWriteTokens != 10 || len(u.Models) != 2 {
		t.Errorf("usage %+v", u)
	}
	if !u.CostKnown || u.CostUSD != 1 || st == nil || st.Model != "claude-sonnet-4.5" {
		t.Errorf("cost %v %v, status %+v", u.CostKnown, u.CostUSD, st)
	}
	if _, _, err := Usage(filepath.Join(t.TempDir(), "events.jsonl")); err == nil {
		t.Error("paths outside Copilot's session state are refused")
	}
}

func TestLastReply(t *testing.T) {
	write(t)
	f, _ := os.OpenFile(EventsPath(id), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant.message","data":{"content":"first"}}
{"type":"assistant.message","data":{"content":"All green.","toolRequests":[]}}
{"type":"assistant.message","data":{"content":"","toolRequests":[{"name":"bash"}]}}
{"type":"assistant.turn_end","data":{}}
`)
	f.Close()
	if got := LastReply(EventsPath(id), 0); got != "All green." {
		t.Errorf("last reply %q", got)
	}
	// A prompt with no reply logged yet: wait, then settle for nothing.
	f, _ = os.OpenFile(EventsPath(id), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user.message","data":{"content":"next"}}` + "\n")
	f.Close()
	if got := LastReply(EventsPath(id), 200*time.Millisecond); got != "" {
		t.Errorf("stale reply %q", got)
	}
}

func TestReadWorkspace(t *testing.T) {
	path := write(t)
	if ws := Workspaces(); len(ws) != 1 || ws[0] != path {
		t.Fatalf("workspaces %v", ws)
	}
	fi, _ := os.Stat(path)
	s, ok := ReadWorkspace(path, fi)
	if !ok || s.ID != id || s.Cwd != "/code/work" || s.Title != "it's a test" || s.StartedAt == 0 {
		t.Fatalf("%+v %v", s, ok)
	}
	if s.Usage == nil || s.Usage.InputTokens != 320 || s.Size == 0 {
		t.Errorf("usage %+v size %d", s.Usage, s.Size)
	}
	// A workspace.yaml whose id isn't its directory's is not a session.
	other := filepath.Join(Home(), "session-state", "other", "workspace.yaml")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte(workspace), 0o644)
	fi, _ = os.Stat(other)
	if _, ok := ReadWorkspace(other, fi); ok {
		t.Error("mismatched id accepted")
	}
}
