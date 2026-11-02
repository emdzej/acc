package hook

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusReportParsesClaudeInput(t *testing.T) {
	in := `{"session_id":"s1","model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},
	"context_window":{"used_percentage":37.4,"context_window_size":200000},
	"rate_limits":{"seven_day":{"used_percentage":42,"resets_at":1793900000},
	               "five_hour":{"used_percentage":80.2,"resets_at":"2026-11-02T13:00:00Z"},
	               "extra":{"used_percentage":null}}}`
	var st claudeStatus
	if err := json.Unmarshal([]byte(in), &st); err != nil {
		t.Fatal(err)
	}
	r := statusReport(st)
	if r.Model != "Opus 5.5" || *r.ContextPct != 37.4 || r.ContextSize != 200000 {
		t.Errorf("report = %+v", r)
	}
	if len(r.Plan) != 2 || r.Plan[0].Label != "5h" || r.Plan[1].Label != "7d" {
		t.Fatalf("plan = %+v", r.Plan)
	}
	if r.Plan[0].ResetsAt != 1793624400000 || r.Plan[1].ResetsAt != 1793900000000 {
		t.Errorf("resets = %d %d", r.Plan[0].ResetsAt, r.Plan[1].ResetsAt)
	}
	line := formatStatus(r)
	for _, want := range []string{"Opus 5.5", "ctx", "37%", "5h", "80%", "7d", "42%"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q missing %q", line, want)
		}
	}
}

func TestSessionStartDoesNotClaimMessages(t *testing.T) {
	if claudeDeliverable["SessionStart"] {
		t.Fatal("SessionStart must not deliver: no turn follows it")
	}
	for _, e := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if !claudeDeliverable[e] {
			t.Errorf("%s should deliver", e)
		}
	}
}
