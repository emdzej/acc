package gemini

import (
	"os"
	"path/filepath"
	"testing"
)

// A chat recording as Gemini CLI 0.66 writes it: the metadata line, its
// own session context, the prompt, a response re-appended once its tool
// call ran, the tool result and the final answer.
const chat = `{"sessionId":"66666666-2222-4333-8444-666666666666","projectHash":"018e","startTime":"2026-11-03T08:32:33.735Z","lastUpdated":"2026-11-03T08:32:33.735Z","kind":"main"}
{"id":"d049","timestamp":"2026-11-03T08:32:33.736Z","type":"user","content":[{"text":"<session_context>\nThis is the Gemini CLI.\n</session_context>"}]}
{"$set":{"lastUpdated":"2026-11-03T08:32:33.741Z"}}
{"id":"e0b8","timestamp":"2026-11-03T08:32:34.154Z","type":"user","content":[{"text":"please use\nshell"}]}
{"id":"ce85","timestamp":"2026-11-03T08:32:34.228Z","type":"gemini","content":"","tokens":{"input":100,"output":7,"cached":20,"thoughts":3,"tool":0,"total":107},"model":"gemini-fake"}
{"id":"ce85","timestamp":"2026-11-03T08:32:34.228Z","type":"gemini","content":"","tokens":{"input":100,"output":7,"cached":20,"thoughts":3,"tool":0,"total":107},"model":"gemini-fake","toolCalls":[{"name":"run_shell_command"}]}
{"id":"29cd","timestamp":"2026-11-03T08:32:34.369Z","type":"user","content":[{"functionResponse":{"name":"run_shell_command","response":{"output":"hello-acc"}}}]}
{"id":"f12c","timestamp":"2026-11-03T08:32:34.382Z","type":"gemini","content":"All done.","tokens":{"input":150,"output":10,"cached":50,"thoughts":0,"tool":0,"total":160},"model":"gemini-2.5-pro"}
`

func write(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	project := filepath.Join(home, ".gemini", "tmp", "work")
	os.MkdirAll(filepath.Join(project, "chats"), 0o755)
	os.WriteFile(filepath.Join(project, ".project_root"), []byte("/code/work"), 0o644)
	path := filepath.Join(project, "chats", "session-2026-11-03T08-32-66666666.jsonl")
	os.WriteFile(path, []byte(chat), 0o644)
	return path
}

func TestUsage(t *testing.T) {
	path := write(t)
	u, st, err := Usage(path)
	if err != nil {
		t.Fatal(err)
	}
	// The re-appended response counts once; cached input is split out,
	// thoughts count as output.
	if u.InputTokens != 80+100 || u.CacheReadTokens != 70 || u.OutputTokens != 10+10 {
		t.Errorf("usage %+v", u)
	}
	if len(u.Models) != 2 || st == nil || st.Model != "gemini-2.5-pro" {
		t.Errorf("models %v, status %+v", u.Models, st)
	}
	if _, _, err := Usage("/etc/passwd.jsonl"); err == nil {
		t.Error("paths outside Gemini's tmp directory are refused")
	}
}

func TestReadSession(t *testing.T) {
	path := write(t)
	if files := SessionFiles(); len(files) != 1 || files[0] != path {
		t.Fatalf("session files %v", files)
	}
	fi, _ := os.Stat(path)
	s, ok := ReadSession(path, fi)
	if !ok || s.ID != "66666666-2222-4333-8444-666666666666" || s.Cwd != "/code/work" || s.Agent != Agent {
		t.Fatalf("%+v %v", s, ok)
	}
	if s.Title != "please use shell" || s.StartedAt == 0 || s.Usage == nil || s.Usage.OutputTokens != 20 {
		t.Errorf("%+v", s)
	}
	// A summary, once Gemini writes one, names the session.
	os.WriteFile(path, []byte(chat+`{"$set":{"summary":"Say hello from the shell"}}`+"\n"), 0o644)
	fi, _ = os.Stat(path)
	if s, _ := ReadSession(path, fi); s.Title != "Say hello from the shell" {
		t.Errorf("title %q", s.Title)
	}
}
