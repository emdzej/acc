package opencode

import (
	"encoding/json"
	"github.com/emdzej/acc/internal/model"
	"testing"
)

func ev(t *testing.T, s string) Result {
	t.Helper()
	var r RawEvent
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}
	return Translate(r)
}

// Payloads captured from OpenCode 2.2.5 (session.created, inbox, step,
// execution) and taken from its event schemas (tools, usage, permission).
func TestTranslate(t *testing.T) {
	r := ev(t, `{"type":"session.created","created":1793629475093,"data":{"sessionID":"ses_1","location":{"directory":"/code/api"},"model":{"id":"beam-free","providerID":"opencode"}}}`)
	if r.Event == nil || r.Event.Name != "SessionStart" || r.Event.Cwd != "/code/api" || r.Model != "beam-free" || r.Event.Agent != Agent {
		t.Fatalf("created: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.inbox.enqueued","data":{"sessionID":"ses_1","item":{"type":"user","payload":{"text":"fix the\ntests"}}}}`)
	if r.Event.Name != "UserPromptSubmit" || r.Event.Summary != "fix the tests" {
		t.Errorf("prompt: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.tool.called","acc_tool":"bash","data":{"sessionID":"ses_1","assistantMessageID":"m","id":"c1","input":{"command":"go test ./..."}}}`)
	if r.Event.Name != "PreToolUse" || r.Event.ToolName != "bash" || r.Event.Summary != "go test ./..." {
		t.Errorf("tool called: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.tool.failed","acc_tool":"bash","data":{"sessionID":"ses_1","id":"c1","error":{"message":"exit 1"}}}`)
	if r.Event.Name != "PostToolUseFailure" {
		t.Errorf("tool failed: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.execution.succeeded","acc_last_text":"All green.","data":{"sessionID":"ses_1"}}`)
	if r.Event.Name != "Stop" || r.Event.Message != "All green." {
		t.Errorf("stop: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.execution.failed","data":{"sessionID":"ses_1","error":{"type":"provider.auth","message":"Provider request failed with HTTP 403"}}}`)
	if r.Event.Name != "Stop" || r.Event.Message != "error: Provider request failed with HTTP 403" {
		t.Errorf("failed: %+v", r.Event)
	}
	r = ev(t, `{"type":"permission.asked","data":{"id":"per_1","sessionID":"ses_1","permission":"bash","patterns":["rm -rf build"]}}`)
	if r.Event.Name != "PermissionRequest" || r.Event.ToolName != "bash" || r.Event.Summary != "rm -rf build" {
		t.Errorf("permission: %+v", r.Event)
	}
	r = ev(t, `{"type":"session.usage.updated","data":{"sessionID":"ses_1","cost":0.25,"tokens":{"input":100,"output":40,"reasoning":10,"cache":{"read":900,"write":5}}}}`)
	if r.Usage == nil || r.Usage.CostUSD != 0.25 || r.Usage.OutputTokens != 50 || r.Usage.CacheReadTokens != 900 {
		t.Errorf("usage: %+v", r.Usage)
	}
	if r := ev(t, `{"type":"session.renamed","data":{"sessionID":"ses_1","title":"Fix flaky tests"}}`); r.Title != "Fix flaky tests" {
		t.Errorf("title: %+v", r)
	}
	if r := ev(t, `{"type":"mcp.status.changed","data":{"server":"x"}}`); r.Event != nil {
		t.Error("non-session event should be ignored")
	}
}

func TestGoal(t *testing.T) {
	res := Translate(RawEvent{Type: "rpc.opencode.goal.changed", Data: []byte(`{"sessionID":"ses_1","goal":{"objective":"tests pass","status":"complete","reason":"done","rounds":4,"maxRounds":50,"updatedAt":5}}`)})
	if !res.GoalSet || res.Goal == nil || res.Goal.Status != model.GoalMet || res.Goal.Objective != "tests pass" || res.Goal.Rounds != 4 {
		t.Fatalf("set: %+v %+v", res, res.Goal)
	}
	res = Translate(RawEvent{Type: "rpc.opencode.goal.changed", Data: []byte(`{"sessionID":"ses_1","goal":null}`)})
	if !res.GoalSet || res.Goal != nil {
		t.Fatalf("cleared: %+v", res)
	}
}
