package hook

import (
	"encoding/json"
	"strings"

	"github.com/emdzej/acc/internal/model"
)

// AgentCodex is OpenAI's Codex CLI, whose hooks are shaped like Claude Code's.
const AgentCodex = "codex"

// codexInput is the subset of Codex's hook stdin payload ACC uses.
type codexInput struct {
	SessionID       string          `json:"session_id"`
	TranscriptPath  string          `json:"transcript_path"`
	Cwd             string          `json:"cwd"`
	HookEventName   string          `json:"hook_event_name"`
	Source          string          `json:"source"`
	Reason          string          `json:"reason"`
	Trigger         string          `json:"trigger"`
	Prompt          string          `json:"prompt"`
	ToolName        string          `json:"tool_name"`
	ToolInput       json.RawMessage `json:"tool_input"`
	LastAssistant   string          `json:"last_assistant_message"`
	AgentID         string          `json:"agent_id"`
	AgentType       string          `json:"agent_type"`
	AgentTranscript string          `json:"agent_transcript_path"`
}

// codexDeliverable: hooks whose output Codex turns into model input, as
// for Claude Code (additionalContext inside a turn; a blocked Stop continues).
var codexDeliverable = map[string]bool{"UserPromptSubmit": true, "PostToolUse": true, "Stop": true}

// parseCodex reads a Codex hook's input. Event names already match ACC's,
// except Interrupt (the user stopped the turn), which ends the turn like Stop.
func parseCodex(raw []byte) (model.Event, error) {
	var ci codexInput
	if err := json.Unmarshal(raw, &ci); err != nil {
		return model.Event{}, err
	}
	ev := model.Event{
		SessionID: ci.SessionID, Name: ci.HookEventName, Cwd: ci.Cwd, TranscriptPath: ci.TranscriptPath,
		ToolName: ci.ToolName, Message: firstNonEmpty(ci.Prompt, ci.LastAssistant),
		AgentID: ci.AgentID, AgentType: ci.AgentType, AgentTranscriptPath: ci.AgentTranscript,
		Deliver: codexDeliverable[ci.HookEventName] && ci.AgentID == "",
	}
	switch ci.HookEventName {
	case "PreToolUse", "PostToolUse", "PermissionRequest":
		ev.Summary = toolSummary(ci.ToolInput)
	case "UserPromptSubmit":
		ev.Summary = ci.Prompt
	case "SessionStart":
		ev.Summary = ci.Source
	case "SessionEnd":
		ev.Summary, ev.Message = ci.Reason, ci.Reason
	case "SubagentStart":
		ev.Summary = ci.AgentType
	case "PreCompact", "PostCompact":
		ev.Summary = ci.Trigger
	case "Interrupt":
		ev.Name, ev.Summary = "Stop", "interrupted"
	}
	ev.Summary = strings.Join(strings.Fields(ev.Summary), " ")
	return ev, nil
}

// toolSummary is a tool input's most telling field: the command (a string
// or an argv list), path, pattern, URL, query, or a patch's first line.
func toolSummary(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil { // e.g. apply_patch's freeform patch
		first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
		return first
	}
	var in map[string]json.RawMessage
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "cmd", "path", "file_path", "pattern", "url", "query", "description", "prompt"} {
		v, ok := in[k]
		if !ok {
			continue
		}
		if json.Unmarshal(v, &s) == nil && s != "" {
			return s
		}
		var argv []string
		if json.Unmarshal(v, &argv) == nil && len(argv) > 0 {
			// ["bash", "-lc", "go test ./..."] shows as the script.
			if len(argv) == 3 && (argv[1] == "-lc" || argv[1] == "-c") {
				return argv[2]
			}
			return strings.Join(argv, " ")
		}
	}
	return ""
}
