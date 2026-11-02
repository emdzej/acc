package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaude(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir) // transcript reads are limited to known config dirs
	proj := filepath.Join(dir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aaa.jsonl", `{"type":"mode"}
{"type":"user","isMeta":true,"cwd":"/w/app","timestamp":"2026-11-01T10:00:00Z","message":{"content":"<command-name>/init</command-name>"}}
{"type":"user","cwd":"/w/app","timestamp":"2026-11-01T10:00:01Z","message":{"content":[{"type":"text","text":"fix   the\nlogin bug"}]}}
`)
	write("bbb.jsonl", `{"type":"user","cwd":"/w/api","timestamp":"2026-11-01T09:00:00Z","message":{"content":"x"}}
{"type":"ai-title","aiTitle":"Rate limits"}
{"type":"custom-title","customTitle":"api limits"}
`)
	write("empty.jsonl", `{"type":"mode"}`+"\n") // no directory: not a session

	// A registry naming bbb as running in this process.
	os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sessionId": "bbb"})
	os.WriteFile(filepath.Join(dir, "sessions", "1.json"), b, 0o644)

	got := map[string]bool{}
	x := NewIndex()
	for _, s := range x.Claude(dir) {
		got[s.ID] = true
		switch s.ID {
		case "aaa":
			if s.Title != "fix the login bug" || s.Cwd != "/w/app" || s.Running || s.StartedAt == 0 {
				t.Errorf("aaa: %+v", s)
			}
		case "bbb":
			if s.Title != "api limits" || !s.Running {
				t.Errorf("bbb: %+v", s)
			}
		}
	}
	if !got["aaa"] || !got["bbb"] || got["empty"] {
		t.Fatalf("sessions: %v", got)
	}
	if len(x.Claude(dir)) != 2 { // cached
		t.Error("second scan differs")
	}
}

func TestOpenCodeMissing(t *testing.T) {
	if s, err := OpenCode(t.Context(), filepath.Join(t.TempDir(), "none.db"), 10); err != nil || s != nil {
		t.Fatalf("%v %v", s, err)
	}
}
