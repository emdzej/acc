package hook

import (
	"errors"
	"testing"

	"github.com/emdzej/acc/internal/model"
)

// Payloads as Copilot CLI 1.0.105 sends them.
func TestParseCopilot(t *testing.T) {
	const id = "55555555-2222-4333-8444-555555555555"
	for _, c := range []struct {
		in, name, tool, summary string
		deliver                 bool
	}{
		{`{"hook_event_name":"SessionStart","session_id":"` + id + `","cwd":"/w","source":"new","initial_prompt":"please use bash"}`, "SessionStart", "", "new", false},
		{`{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `","cwd":"/w","prompt":"please use bash"}`, "UserPromptSubmit", "", "please use bash", true},
		{`{"hook_event_name":"PreToolUse","session_id":"` + id + `","tool_name":"Bash","tool_input":{"command":"echo hello-acc","description":"say hello"}}`, "PreToolUse", "Bash", "echo hello-acc", false},
		{`{"hookName":"permissionRequest","sessionId":"` + id + `","timestamp":1793694336105,"cwd":"/w","toolName":"bash","toolInput":{"command":"echo hello-acc"},"permissionSuggestions":[]}`, "PermissionRequest", "bash", "echo hello-acc", false},
		{`{"hook_event_name":"PostToolUse","session_id":"` + id + `","tool_name":"Bash","tool_input":{"command":"echo hello-acc"},"tool_result":{"result_type":"success","text_result_for_llm":"hello-acc"}}`, "PostToolUse", "Bash", "echo hello-acc", true},
		{`{"hook_event_name":"PostToolUseFailure","session_id":"` + id + `","tool_name":"Bash","tool_input":{"command":"false"},"error":{"message":"exit 1"}}`, "PostToolUseFailure", "Bash", "false", false},
		{`{"hook_event_name":"Notification","session_id":"` + id + `","notification_type":"permission_prompt","message":"Allow bash?"}`, "Notification", "", "Allow bash?", false},
		{`{"hook_event_name":"ErrorOccurred","session_id":"` + id + `","error":{"message":"quota exceeded"},"recoverable":false}`, "Notification", "", "quota exceeded", false},
		{`{"hook_event_name":"Stop","session_id":"` + id + `","transcript_path":"/t/events.jsonl","stop_reason":"end_turn","stop_hook_active":false}`, "Stop", "", "", true},
		{`{"hook_event_name":"SubagentStop","session_id":"` + id + `","agent_id":"a1","agent_type":"explore"}`, "SubagentStop", "", "", false},
		{`{"hook_event_name":"SessionEnd","session_id":"` + id + `","reason":"complete"}`, "SessionEnd", "", "complete", false},
	} {
		ev, err := parseCopilot([]byte(c.in))
		if err != nil || ev.Name != c.name || ev.ToolName != c.tool || ev.Summary != c.summary || ev.Deliver != c.deliver || ev.SessionID != id {
			t.Errorf("%s:\n got %+v %v", c.in, ev, err)
		}
		if ev.TranscriptPath == "" {
			t.Errorf("%s: no transcript path", c.in)
		}
	}
	for _, in := range []string{
		`{"hook_event_name":"Notification","session_id":"s","notification_type":"agent_idle","message":"idle"}`,
		`{"hook_event_name":"ErrorOccurred","session_id":"s","error":"blip","recoverable":true}`,
	} {
		if _, err := parseCopilot([]byte(in)); !errors.Is(err, errSkip) {
			t.Errorf("%s: want skip, got %v", in, err)
		}
	}
	ev, _ := parseCopilot([]byte(`{"hook_event_name":"Stop","session_id":"s","transcript_path":"/t/events.jsonl"}`))
	if ev.TranscriptPath != "/t/events.jsonl" {
		t.Errorf("transcript path %q", ev.TranscriptPath)
	}
	if _, ok := harnesses[AgentCopilot]; !ok {
		t.Error("copilot harness not registered")
	}
}

func TestParseGemini(t *testing.T) {
	for _, c := range []struct {
		in, name, tool, summary, msg string
		deliver                      bool
	}{
		{`{"session_id":"g1","transcript_path":"/t.jsonl","cwd":"/w","hook_event_name":"SessionStart","source":"startup"}`, "SessionStart", "", "startup", "", false},
		{`{"session_id":"g1","hook_event_name":"BeforeAgent","prompt":"list\nfiles"}`, "UserPromptSubmit", "", "list files", "list\nfiles", true},
		{`{"session_id":"g1","hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"ls -la"}}`, "PreToolUse", "run_shell_command", "ls -la", "", false},
		{`{"session_id":"g1","hook_event_name":"AfterTool","tool_name":"run_shell_command","tool_input":{"command":"ls"},"tool_response":{"llmContent":"a.go","returnDisplay":"a.go"}}`, "PostToolUse", "run_shell_command", "ls", "a.go", true},
		{`{"session_id":"g1","hook_event_name":"AfterTool","tool_name":"read_file","tool_input":{"file_path":"x"},"tool_response":{"llmContent":"","error":"no such file"}}`, "PostToolUseFailure", "read_file", "x", "no such file", true},
		{`{"session_id":"g1","hook_event_name":"Notification","notification_type":"ToolPermission","message":"Tool needs confirmation","details":{"type":"exec","title":"Confirm Shell Command","command":"rm -rf build"}}`, "Notification", "", "rm -rf build", "rm -rf build", false},
		{`{"session_id":"g1","hook_event_name":"AfterAgent","prompt":"hi","prompt_response":"Done.","stop_hook_active":false}`, "Stop", "", "", "Done.", true},
		{`{"session_id":"g1","hook_event_name":"SessionEnd","reason":"exit"}`, "SessionEnd", "", "exit", "exit", false},
	} {
		ev, err := parseGemini([]byte(c.in))
		if err != nil || ev.Name != c.name || ev.ToolName != c.tool || ev.Summary != c.summary || ev.Message != c.msg || ev.Deliver != c.deliver || ev.SessionID != "g1" {
			t.Errorf("%s:\n got %+v %v", c.in, ev, err)
		}
	}
	ev, _ := parseGemini([]byte(`{"session_id":"g1","hook_event_name":"Notification","notification_type":"ToolPermission","message":"m"}`))
	if ev.NotificationType != "permission_prompt" {
		t.Errorf("tool permission is a permission prompt, got %q", ev.NotificationType)
	}
	if _, err := parseGemini([]byte(`{"session_id":"g1","hook_event_name":"BeforeModel"}`)); !errors.Is(err, errSkip) {
		t.Errorf("unsubscribed events are skipped, got %v", err)
	}

	// Output uses Gemini's event names.
	out := geminiOutput(model.Event{Name: "PostToolUse"}, []model.Message{{Body: "hi"}}).(map[string]any)
	if h := out["hookSpecificOutput"].(map[string]any); h["hookEventName"] != "AfterTool" || h["additionalContext"] == "" {
		t.Errorf("after tool output %v", out)
	}
	out = geminiOutput(model.Event{Name: "Stop"}, []model.Message{{Body: "hi"}}).(map[string]any)
	if out["decision"] != "block" {
		t.Errorf("stop output %v", out)
	}
}
