package hook

import (
	"strings"
	"testing"
)

func TestParseCodex(t *testing.T) {
	for _, c := range []struct {
		in, name, tool, summary string
		deliver                 bool
	}{
		{`{"session_id":"019a","transcript_path":"/t.jsonl","cwd":"/code","hook_event_name":"SessionStart","model":"gpt-5.5","source":"startup"}`, "SessionStart", "", "startup", false},
		{`{"session_id":"019a","hook_event_name":"UserPromptSubmit","turn_id":"t1","prompt":"fix the\ntests"}`, "UserPromptSubmit", "", "fix the tests", true},
		{`{"session_id":"019a","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`, "PreToolUse", "Bash", "go test ./...", false},
		{`{"session_id":"019a","hook_event_name":"PreToolUse","tool_name":"shell","tool_input":{"command":["bash","-lc","ls -la"]}}`, "PreToolUse", "shell", "ls -la", false},
		{`{"session_id":"019a","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build","description":"clean"}}`, "PermissionRequest", "Bash", "rm -rf build", false},
		{`{"session_id":"019a","hook_event_name":"PostToolUse","tool_name":"apply_patch","tool_input":"*** Begin Patch\n*** Update File: a.go","tool_response":"ok"}`, "PostToolUse", "apply_patch", "*** Begin Patch", true},
		{`{"session_id":"019a","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"All green."}`, "Stop", "", "", true},
		{`{"session_id":"019a","hook_event_name":"Interrupt","turn_id":"t1"}`, "Stop", "", "interrupted", false},
		{`{"session_id":"019a","hook_event_name":"SubagentStop","agent_id":"a1","agent_type":"explorer","last_assistant_message":"found it"}`, "SubagentStop", "", "", false},
		{`{"session_id":"019a","hook_event_name":"SessionEnd","reason":"other"}`, "SessionEnd", "", "other", false},
	} {
		ev, err := parseCodex([]byte(c.in))
		if err != nil || ev.Name != c.name || ev.ToolName != c.tool || ev.Summary != c.summary || ev.Deliver != c.deliver || ev.SessionID != "019a" {
			t.Errorf("%s:\n got %+v %v", c.in, ev, err)
		}
	}
	ev, _ := parseCodex([]byte(`{"session_id":"019a","hook_event_name":"Stop","last_assistant_message":"All green."}`))
	if ev.Message != "All green." {
		t.Errorf("stop message %q", ev.Message)
	}
	if _, ok := harnesses[AgentCodex]; !ok || !strings.Contains(strings.Join(Agents(), ","), "codex") {
		t.Error("codex harness not registered")
	}
}
