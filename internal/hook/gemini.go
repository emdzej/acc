package hook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emdzej/acc/internal/model"
)

// AgentGemini is Google's Gemini CLI.
const AgentGemini = "gemini"

// geminiNames maps Gemini's hook events to ACC's canonical events.
var geminiNames = map[string]string{
	"SessionStart": "SessionStart",
	"BeforeAgent":  "UserPromptSubmit",
	"BeforeTool":   "PreToolUse",
	"AfterTool":    "PostToolUse",
	"Notification": "Notification",
	"AfterAgent":   "Stop",
	"SessionEnd":   "SessionEnd",
}

type geminiInput struct {
	SessionID        string          `json:"session_id"`
	TranscriptPath   string          `json:"transcript_path"`
	Cwd              string          `json:"cwd"`
	HookEventName    string          `json:"hook_event_name"`
	Source           string          `json:"source"`
	Reason           string          `json:"reason"`
	Prompt           string          `json:"prompt"`
	PromptResponse   string          `json:"prompt_response"`
	StopHookActive   bool            `json:"stop_hook_active"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	Details          struct {
		Title   string `json:"title"`
		Command string `json:"command"`
	} `json:"details"`
	ToolResponse struct {
		LLMContent json.RawMessage `json:"llmContent"`
		Error      json.RawMessage `json:"error"`
	} `json:"tool_response"`
}

// geminiDeliverable: BeforeAgent and AfterTool take additionalContext; a
// blocked AfterAgent makes Gemini run the reason as the next prompt.
var geminiDeliverable = map[string]bool{"BeforeAgent": true, "AfterTool": true, "AfterAgent": true}

func parseGemini(raw []byte) (model.Event, error) {
	var gi geminiInput
	if err := json.Unmarshal(raw, &gi); err != nil {
		return model.Event{}, err
	}
	name, ok := geminiNames[gi.HookEventName]
	if !ok {
		return model.Event{}, errSkip
	}
	ev := model.Event{
		SessionID: gi.SessionID, Name: name, Cwd: gi.Cwd, TranscriptPath: gi.TranscriptPath, ToolName: gi.ToolName,
		Deliver: geminiDeliverable[gi.HookEventName],
	}
	switch gi.HookEventName {
	case "SessionStart":
		ev.Summary = gi.Source
	case "BeforeAgent":
		ev.Message, ev.Summary = gi.Prompt, gi.Prompt
	case "BeforeTool":
		ev.Summary = toolSummary(gi.ToolInput)
	case "AfterTool":
		ev.Summary = toolSummary(gi.ToolInput)
		ev.Message = toolText(gi.ToolResponse.LLMContent)
		if len(gi.ToolResponse.Error) > 0 && string(gi.ToolResponse.Error) != "null" {
			ev.Name, ev.Message = "PostToolUseFailure", firstNonEmpty(errorText(gi.ToolResponse.Error), ev.Message)
		}
	case "Notification":
		// Gemini notifies only when a tool call waits for approval.
		ev.NotificationType = "permission_prompt"
		if gi.NotificationType != "ToolPermission" {
			ev.NotificationType = strings.ToLower(gi.NotificationType)
		}
		ev.Message = firstNonEmpty(gi.Details.Command, gi.Details.Title, gi.Message)
		ev.Summary = ev.Message
	case "AfterAgent":
		ev.Message = gi.PromptResponse
	case "SessionEnd":
		ev.Summary, ev.Message = gi.Reason, gi.Reason
	}
	ev.Summary = strings.Join(strings.Fields(ev.Summary), " ")
	return ev, nil
}

// toolText is a tool result's text: a string, or parts with text.
func toolText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &parts)
	var out []string
	for _, p := range parts {
		out = append(out, p.Text)
	}
	return strings.Join(out, "\n")
}

// geminiOutput injects messages like Claude Code's hooks, under Gemini's
// event names: additionalContext before a turn or after a tool, and a
// blocked AfterAgent to run them as the next prompt.
func geminiOutput(ev model.Event, msgs []model.Message) any {
	text := messagesText(msgs)
	if ev.Name == "Stop" {
		// Gemini shows a block as a warning; systemMessage replaces the
		// reason (which becomes the prompt) there.
		note := "delivering a message from Agents Control Centre"
		if len(msgs) > 1 {
			note = fmt.Sprintf("delivering %d messages from Agents Control Centre", len(msgs))
		}
		return map[string]any{"decision": "block", "reason": text, "systemMessage": note}
	}
	event := "BeforeAgent"
	if ev.Name == "PostToolUse" || ev.Name == "PostToolUseFailure" {
		event = "AfterTool"
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text}}
}
