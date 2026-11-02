package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `{"type":"user","timestamp":"2026-11-02T10:00:00Z","message":{"role":"user","content":"fix the test"}}
{"type":"attachment","attachment":{}}
{"type":"assistant","timestamp":"2026-11-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"Running tests."},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","timestamp":"2026-11-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"FAIL"}],"is_error":true}]}}
`

func TestReadFollowsCompleteLinesOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := filepath.Join(root, "projects", "p", "s.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(sample+`{"type":"assistant","message":`), 0o600) // partial trailing line

	page, err := Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := ""
	for _, e := range page.Entries {
		kinds += e.Kind + " "
	}
	if kinds != "prompt text tool_use tool_result " {
		t.Fatalf("kinds = %q", kinds)
	}
	if e := page.Entries[2]; e.Tool != "Bash" || e.Text != "go test ./..." {
		t.Errorf("tool_use = %+v", e)
	}
	if !page.Entries[3].Error || page.Entries[3].Text != "FAIL" {
		t.Errorf("tool_result = %+v", page.Entries[3])
	}

	// Completing the partial line yields exactly the new entry.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"role":"assistant","content":[{"type":"text","text":"Fixed."}]}}` + "\n")
	f.Close()
	next, err := Read(path, page.Offset)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 1 || next.Entries[0].Text != "Fixed." {
		t.Fatalf("follow = %+v", next.Entries)
	}
}

func TestReadRejectsPathsOutsideRoots(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	outside := filepath.Join(t.TempDir(), "x.jsonl")
	os.WriteFile(outside, []byte("{}\n"), 0o600)
	if _, err := Read(outside, 0); err != ErrForbidden {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	if _, err := Read("/etc/passwd", 0); err != ErrForbidden {
		t.Fatalf("want ErrForbidden for non-jsonl, got %v", err)
	}
}

func TestFromPayload(t *testing.T) {
	l := FromPayload("PostToolUse", 1, []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"","stderr":""}}`))
	if l.Kind != "tool" || l.Tool != "Bash" || l.Input != "ls" || l.Output != "" {
		t.Errorf("bash = %+v", l)
	}
	l = FromPayload("Stop", 1, []byte(`{"last_assistant_message":"DONE"}`))
	if l.Kind != "message" || l.Text != "DONE" {
		t.Errorf("stop = %+v", l)
	}
}

func TestTitlesLatestWins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := filepath.Join(root, "projects", "p", "s.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Fix flaky test"}
{"type":"custom-title","customTitle":"old-name"}
{"type":"user","message":{"role":"user","content":"x"}}
{"type":"custom-title","customTitle":"api-refactor"}
`), 0o600)
	custom, ai, err := Titles(path, false)
	if err != nil || custom != "api-refactor" || ai != "Fix flaky test" {
		t.Fatalf("got %q %q %v", custom, ai, err)
	}
}
