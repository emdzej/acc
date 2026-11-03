package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tr(t *testing.T, s string) Result {
	t.Helper()
	var r RawEvent
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}
	return Translate(r)
}

const sess = `"session":{"id":"019de4a9","file":"/s/a.jsonl","cwd":"/code/api","name":""}`

func TestTranslate(t *testing.T) {
	r := tr(t, `{"type":"session_start","ts":5,`+sess+`,"model":"anthropic/claude-opus-5-5","data":{"reason":"startup"}}`)
	if e := r.Event; e == nil || e.Name != "SessionStart" || e.Cwd != "/code/api" || e.TranscriptPath != "/s/a.jsonl" || e.Agent != Agent || e.TS != 5 || r.Model != "anthropic/claude-opus-5-5" {
		t.Fatalf("start: %+v %+v", r, r.Event)
	}
	for _, c := range []struct{ in, name, tool, summary, msg string }{
		{`{"type":"input",` + sess + `,"data":{"text":"fix the\ntests","source":"interactive"}}`, "UserPromptSubmit", "", "fix the tests", "fix the\ntests"},
		{`{"type":"tool_execution_start",` + sess + `,"data":{"toolName":"bash","args":{"command":"go test ./..."}}}`, "PreToolUse", "bash", "go test ./...", ""},
		{`{"type":"tool_execution_end",` + sess + `,"data":{"toolName":"bash","isError":true,"result":"exit 1"}}`, "PostToolUseFailure", "bash", "", "exit 1"},
		{`{"type":"tool_execution_end",` + sess + `,"data":{"toolName":"read","result":"ok"}}`, "PostToolUse", "read", "", "ok"},
		{`{"type":"ui_prompt_start",` + sess + `,"data":{"kind":"confirm","title":"Allow rm -rf build?"}}`, "PermissionRequest", "", "", "Allow rm -rf build?"},
		{`{"type":"ui_prompt_start",` + sess + `,"data":{"kind":"select","title":"Pick a branch"}}`, "Question", "", "", "Pick a branch"},
		{`{"type":"ui_prompt_end",` + sess + `}`, "QuestionAnswered", "", "", ""},
		{`{"type":"agent_settled",` + sess + `,"data":{"lastText":"All green.","aborted":false}}`, "Stop", "", "", "All green."},
		{`{"type":"session_shutdown",` + sess + `,"data":{"reason":"quit"}}`, "SessionEnd", "", "quit", "quit"},
	} {
		e := tr(t, c.in).Event
		if e == nil || e.Name != c.name || e.ToolName != c.tool || e.Summary != c.summary || e.Message != c.msg {
			t.Errorf("%s: got %+v", c.in, e)
		}
	}
	// A reload keeps the same session going.
	if r := tr(t, `{"type":"session_shutdown",`+sess+`,"data":{"reason":"reload"}}`); r.Event != nil {
		t.Errorf("reload ended the session: %+v", r.Event)
	}
	r = tr(t, `{"type":"session_info_changed",`+sess+`,"data":{"name":"Refactor auth"}}`)
	if r.Event != nil || r.Title != "Refactor auth" {
		t.Errorf("rename: %+v", r)
	}
	r = tr(t, `{"type":"agent_settled",`+sess+`,"context":{"tokens":50000,"contextWindow":200000,"percent":25},"data":{}}`)
	if r.ContextPct == nil || *r.ContextPct != 25 || r.ContextSize != 200000 {
		t.Errorf("context: %+v", r)
	}
	if r := tr(t, `{"type":"message_update",`+sess+`}`); r.Event != nil {
		t.Error("unknown events are ignored")
	}
	if r := tr(t, `{"type":"input","session":{"id":""}}`); r.Event != nil {
		t.Error("events without a session are ignored")
	}
}

// A session file in pi's format (docs/session-format.md, version 3).
const sessionFile = `{"type":"session","version":3,"id":"019de4a9-46d7","timestamp":"2026-05-01T17:49:52.216Z","cwd":"/code/api"}
{"type":"model_change","id":"a1","parentId":null,"timestamp":"2026-05-01T17:49:52.223Z","provider":"anthropic","modelId":"claude-opus-5-5"}
{"type":"message","id":"a2","parentId":"a1","timestamp":"2026-05-01T17:50:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Port the  legacy\nimporter"}],"timestamp":1}}
{"type":"message","id":"a3","parentId":"a2","timestamp":"2026-05-01T17:50:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Sure."}],"model":"claude-opus-5-5","usage":{"input":100,"output":20,"cacheRead":1000,"cacheWrite":50,"totalTokens":1170,"cost":{"total":0.25}}}}
{"type":"usage","id":"a4","parentId":"a3","timestamp":"2026-05-01T17:55:00.000Z","kind":"cache_warm","model":"claude-opus-5-5","usage":{"input":0,"output":0,"cacheRead":5000,"cacheWrite":0,"cost":{"total":0.01}}}
{"type":"message","id":"a5","parentId":"a2","timestamp":"2026-05-01T17:56:00.000Z","message":{"role":"assistant","content":"Another branch.","model":"gpt-5.4","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.04}}}}
{"type":"session_info","id":"a6","parentId":"a5","timestamp":"2026-05-01T17:57:00.000Z","name":"Legacy importer"}
not json
`

func TestSessionFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", dir)
	path := filepath.Join(dir, "--code-api--", "2026-05-01T17-49-52-216Z_019de4a9-46d7.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(sessionFile), 0o644)
	os.WriteFile(filepath.Join(dir, "--code-api--", "stub.jsonl"), []byte("{}\n"), 0o644)

	files := SessionFiles(SessionsDir())
	if len(files) != 2 {
		t.Fatalf("files: %v", files)
	}
	fi, _ := os.Stat(path)
	s, ok := ReadSession(path, fi)
	if !ok || s.ID != "019de4a9-46d7" || s.Cwd != "/code/api" || s.Title != "Legacy importer" || s.StartedAt == 0 || s.Agent != Agent {
		t.Fatalf("session: %+v", s)
	}
	// Every branch counts, plus non-message usage such as cache warming.
	u := s.Usage
	if u.InputTokens != 110 || u.OutputTokens != 25 || u.CacheReadTokens != 6000 || u.CacheWriteTokens != 50 || u.CostUSD < 0.2999 || u.CostUSD > 0.3001 || len(u.Models) != 2 {
		t.Errorf("usage: %+v", u)
	}
	if got, err := Usage(path); err != nil || got.CostUSD != u.CostUSD {
		t.Errorf("Usage: %+v %v", got, err)
	}
	stub, _ := os.Stat(filepath.Join(dir, "--code-api--", "stub.jsonl"))
	if _, ok := ReadSession(filepath.Join(dir, "--code-api--", "stub.jsonl"), stub); ok {
		t.Error("a file without a session header isn't a session")
	}

	// Without a name, the first prompt is the title.
	os.WriteFile(path, []byte(sessionFile[:len(sessionFile)-len("{\"type\":\"session_info\",\"id\":\"a6\",\"parentId\":\"a5\",\"timestamp\":\"2026-05-01T17:57:00.000Z\",\"name\":\"Legacy importer\"}\nnot json\n")]), 0o644)
	fi, _ = os.Stat(path)
	if s, _ := ReadSession(path, fi); s.Title != "Port the legacy importer" {
		t.Errorf("title from prompt: %q", s.Title)
	}

	// Usage only reads pi's own session files: the path comes from an event.
	other := filepath.Join(t.TempDir(), "x.jsonl")
	os.WriteFile(other, []byte(sessionFile), 0o644)
	if _, err := Usage(other); err != ErrForbidden {
		t.Errorf("outside the sessions dir: %v", err)
	}
}

// The extension sends pi's real session against this file layout; check
// the reader on the machine's own sessions too, when there are any.
func TestLocalSessions(t *testing.T) {
	files := SessionFiles(SessionsDir())
	if len(files) == 0 {
		t.Skip("no local pi sessions")
	}
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		if s, ok := ReadSession(f, fi); ok && (s.ID == "" || s.Cwd == "" || s.Usage == nil) {
			t.Errorf("%s: incomplete %+v", f, s)
		}
	}
}
