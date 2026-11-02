package transcript

import (
	"github.com/emdzej/acc/internal/model"
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
	m, err := ReadMeta(path, false)
	if err != nil || m.Custom != "api-refactor" || m.AI != "Fix flaky test" || m.GoalSeen {
		t.Fatalf("got %+v %v", m, err)
	}
}

func TestGoal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := filepath.Join(root, "projects", "p", "s.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	set := `{"type":"attachment","timestamp":"2026-11-02T12:00:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"all tests pass"}}` + "\n"
	check := `{"type":"attachment","timestamp":"2026-11-02T12:05:00Z","attachment":{"type":"goal_status","met":false,"condition":"all tests pass","reason":"2 tests still fail"}}` + "\n"
	met := `{"type":"attachment","attachment":{"type":"goal_status","met":true,"condition":"all tests pass","reason":"go test passes","iterations":3}}` + "\n"
	cleared := `{"type":"attachment","attachment":{"type":"goal_status","met":true,"sentinel":true,"condition":"all tests pass"}}` + "\n"
	for _, c := range []struct {
		lines          string
		status, reason string
	}{
		{set, model.GoalActive, ""},
		{set + check, model.GoalActive, "2 tests still fail"},
		{set + check + met, model.GoalMet, "go test passes"},
		{set + cleared, "", ""},
	} {
		os.WriteFile(path, []byte(c.lines), 0o600)
		m, err := ReadMeta(path, true)
		if err != nil || !m.GoalSeen {
			t.Fatalf("%v %+v", err, m)
		}
		if c.status == "" {
			if m.Goal != nil {
				t.Errorf("cleared goal: %+v", m.Goal)
			}
			continue
		}
		if m.Goal == nil || m.Goal.Status != c.status || m.Goal.Reason != c.reason || m.Goal.Objective != "all tests pass" {
			t.Errorf("want %s %q, got %+v", c.status, c.reason, m.Goal)
		}
	}
}

func TestUnwrapChannel(t *testing.T) {
	src, body := Unwrap("<channel source=\"acc\" message_id=\"4\" sender=\"local\" sent_at=\"2026-11-02T13:18:41+01:00\">\nresume\nnow\n</channel>")
	if src != "acc" || body != "resume\nnow" {
		t.Errorf("got %q %q", src, body)
	}
	if src, body := Unwrap("plain prompt"); src != "" || body != "plain prompt" {
		t.Errorf("plain: %q %q", src, body)
	}
	e := parseLine([]byte(`{"type":"user","timestamp":"2026-11-02T12:33:52.294Z","message":{"role":"user","content":"<channel source=\"acc\" message_id=\"5\">\nresume\n</channel>"}}`))
	if len(e) != 1 || e[0].Source != "acc" || e[0].Text != "resume" {
		t.Errorf("parseLine: %+v", e)
	}
}
