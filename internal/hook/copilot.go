package hook

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/copilot"
	"github.com/emdzej/acc/internal/model"
)

// AgentCopilot is GitHub Copilot CLI.
const AgentCopilot = "copilot"

// copilotInput is Copilot's hook payload. ACC registers PascalCase event
// names, which get snake_case payloads, but Copilot 1.0.105 still sends
// PermissionRequest in camelCase, so both spellings are read.
type copilotInput struct {
	SessionID        string          `json:"session_id"`
	SessionIDCamel   string          `json:"sessionId"`
	HookEventName    string          `json:"hook_event_name"`
	HookName         string          `json:"hookName"`
	Cwd              string          `json:"cwd"`
	TranscriptPath   string          `json:"transcript_path"`
	Source           string          `json:"source"`
	InitialPrompt    string          `json:"initial_prompt"`
	Prompt           string          `json:"prompt"`
	Reason           string          `json:"reason"`
	ToolName         string          `json:"tool_name"`
	ToolNameCamel    string          `json:"toolName"`
	ToolInput        json.RawMessage `json:"tool_input"`
	ToolInputCamel   json.RawMessage `json:"toolInput"`
	Error            json.RawMessage `json:"error"`
	Recoverable      bool            `json:"recoverable"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	Trigger          string          `json:"trigger"`
	AgentID          string          `json:"agent_id"`
	AgentType        string          `json:"agent_type"`
	LastAssistant    string          `json:"last_assistant_message"`
	ToolResult       struct {
		Text string `json:"text_result_for_llm"`
	} `json:"tool_result"`
}

// copilotDeliverable: hooks whose additionalContext (or, for Stop, a block
// reason) reaches the model.
var copilotDeliverable = map[string]bool{"UserPromptSubmit": true, "PostToolUse": true, "Stop": true}

// Copilot notifications ACC passes on; the rest (finished shell jobs, "agent
// idle" right after Stop) add nothing to what hooks already said.
var copilotNotifications = map[string]bool{"permission_prompt": true, "elicitation_dialog": true}

func parseCopilot(raw []byte) (model.Event, error) {
	var ci copilotInput
	if err := json.Unmarshal(raw, &ci); err != nil {
		return model.Event{}, err
	}
	name := ci.HookEventName
	if name == "" && ci.HookName != "" { // camelCase payload: permissionRequest -> PermissionRequest
		name = strings.ToUpper(ci.HookName[:1]) + ci.HookName[1:]
	}
	ev := model.Event{
		SessionID: firstNonEmpty(ci.SessionID, ci.SessionIDCamel), Name: name, Cwd: ci.Cwd,
		TranscriptPath: ci.TranscriptPath, ToolName: firstNonEmpty(ci.ToolName, ci.ToolNameCamel),
		NotificationType: ci.NotificationType, AgentID: ci.AgentID, AgentType: ci.AgentType,
		Deliver: copilotDeliverable[name] && ci.AgentID == "",
	}
	if ev.TranscriptPath == "" && ev.SessionID != "" {
		// Only some hooks name the event log; it is always in the same place.
		ev.TranscriptPath = copilot.EventsPath(ev.SessionID)
	}
	input := ci.ToolInput
	if len(input) == 0 {
		input = ci.ToolInputCamel
	}
	switch name {
	case "SessionStart":
		ev.Summary, ev.Message = ci.Source, ci.InitialPrompt
	case "UserPromptSubmit":
		ev.Message, ev.Summary = ci.Prompt, ci.Prompt
	case "PreToolUse", "PermissionRequest":
		ev.Summary = toolSummary(input)
	case "PostToolUse":
		ev.Summary, ev.Message = toolSummary(input), ci.ToolResult.Text
	case "PostToolUseFailure":
		ev.Summary, ev.Message = toolSummary(input), errorText(ci.Error)
	case "Notification":
		if !copilotNotifications[ci.NotificationType] {
			return model.Event{}, errSkip
		}
		ev.Message, ev.Summary = ci.Message, ci.Message
	case "ErrorOccurred":
		if ci.Recoverable {
			return model.Event{}, errSkip
		}
		// An unrecoverable error stops the agent: it needs you.
		ev.Name, ev.NotificationType, ev.Message = "Notification", "error", errorText(ci.Error)
		ev.Summary = ev.Message
	case "Stop", "SubagentStop":
		ev.Message = ci.LastAssistant
		if ev.Message == "" && name == "Stop" {
			ev.Message = copilot.LastReply(ev.TranscriptPath, 1500*time.Millisecond)
		}
	case "SessionEnd":
		ev.Summary, ev.Message = ci.Reason, ci.Reason
	case "PreCompact":
		ev.Summary = ci.Trigger
	}
	ev.Summary = strings.Join(strings.Fields(ev.Summary), " ")
	return ev, nil
}

// errorText is an error field's message: a string or {"message": …}.
func errorText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var e struct {
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &e)
	return e.Message
}

// copilotOutput injects messages: additionalContext inside a turn; a
// blocked Stop makes Copilot take another turn with the reason as prompt.
func copilotOutput(ev model.Event, msgs []model.Message) any {
	text := messagesText(msgs)
	if ev.Name == "Stop" {
		return map[string]any{"decision": "block", "reason": text}
	}
	return map[string]any{"additionalContext": text}
}
