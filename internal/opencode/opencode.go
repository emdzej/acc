// Package opencode translates OpenCode (2.x) plugin events into ACC's
// session model. The ACC plugin (see internal/setup) forwards raw events;
// keeping the mapping here, in Go, makes it testable and lets ACC follow
// OpenCode changes without reinstalling the plugin.
package opencode

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

const Agent = "opencode"

// RawEvent is an OpenCode bus event as the plugin forwards it.
type RawEvent struct {
	Type     string `json:"type"`
	Created  int64  `json:"created"` // unix ms
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Data json.RawMessage `json:"data"`
	// Added by the ACC plugin: the tool name for tool events (OpenCode only
	// names a tool when its input starts), and the turn's last text.
	ACCTool     string `json:"acc_tool,omitempty"`
	ACCLastText string `json:"acc_last_text,omitempty"`
}

// Result is what one event changes in ACC.
type Result struct {
	Event *model.Event // a lifecycle event to record, if any
	Title string       // session title (session.renamed)
	Usage *model.Usage // running totals (session.usage.updated)
	Model string       // model in use (session.step.started)
	// Goal is the session's /goal (rpc.opencode.goal.changed); GoalSet says
	// the event carried one, since a nil Goal means it was cleared.
	Goal    *model.Goal
	GoalSet bool
}

// goalState is OpenCode's Goal.State.
type goalState struct {
	Objective string `json:"objective"`
	Status    string `json:"status"` // active, paused, complete
	Reason    string `json:"reason"`
	Rounds    int    `json:"rounds"`
	MaxRounds int    `json:"maxRounds"`
	UpdatedAt int64  `json:"updatedAt"`
}

type data struct {
	SessionID string `json:"sessionID"`
	Location  struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Model struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model"`
	Item struct {
		Payload struct {
			Text string `json:"text"`
		} `json:"payload"`
	} `json:"item"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Text       string   `json:"text"`
	Title      string   `json:"title"`
	Permission string   `json:"permission"`
	Patterns   []string `json:"patterns"`
	Reason     string   `json:"reason"`
	Cost       float64  `json:"cost"`
	Tokens     struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Goal *goalState `json:"goal"`
	Form *struct {
		SessionID string `json:"sessionID"`
		Title     string `json:"title"`
	} `json:"form"`
}

// Translate maps one OpenCode event. Unknown or uninteresting events give
// an empty Result.
func Translate(raw RawEvent) Result {
	var d data
	if len(raw.Data) > 0 && json.Unmarshal(raw.Data, &d) != nil {
		return Result{}
	}
	sid := d.SessionID
	if sid == "" && d.Form != nil {
		sid = d.Form.SessionID
	}
	if sid == "" {
		return Result{}
	}
	ts := raw.Created
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	cwd := firstNonEmpty(d.Location.Directory, raw.Location.Directory)
	ev := &model.Event{Agent: Agent, SessionID: sid, Cwd: cwd, TS: ts}
	// Raw is Claude-shaped so ACC's "latest activity" view reads both agents.
	payload := map[string]any{}

	switch raw.Type {
	case "session.created":
		ev.Name = "SessionStart"
		ev.Summary = d.Model.ID
		return Result{Event: withRaw(ev, payload), Model: d.Model.ID}
	case "session.inbox.enqueued":
		ev.Name = "UserPromptSubmit"
		ev.Message = d.Item.Payload.Text
		ev.Summary = oneLine(d.Item.Payload.Text)
		payload["prompt"] = d.Item.Payload.Text
	case "session.execution.started":
		ev.Name = "UserPromptSubmit" // the turn is running: working, attention cleared
	case "session.step.started":
		return Result{Model: d.Model.ID}
	case "session.tool.called":
		ev.Name, ev.ToolName = "PreToolUse", raw.ACCTool
		ev.Summary = transcript.ToolInputSummary(d.Input)
		payload["tool_name"], payload["tool_input"] = raw.ACCTool, d.Input
	case "session.tool.success", "session.tool.failed":
		ev.Name, ev.ToolName = "PostToolUse", raw.ACCTool
		payload["tool_name"], payload["tool_response"] = raw.ACCTool, d.Content
		if raw.Type == "session.tool.failed" {
			ev.Name = "PostToolUseFailure"
			if d.Error != nil {
				payload["error"] = d.Error.Message
			}
		}
	case "session.text.ended":
		ev.Name = "AssistantText" // keeps the last message fresh mid-turn
		ev.Message = d.Text
	case "session.execution.succeeded", "session.execution.failed", "session.execution.interrupted":
		ev.Name = "Stop"
		ev.Message = raw.ACCLastText
		if d.Error != nil && d.Error.Message != "" {
			ev.Message = "error: " + d.Error.Message
		} else if d.Reason != "" && raw.ACCLastText == "" {
			ev.Message = "interrupted (" + d.Reason + ")"
		}
		payload["last_assistant_message"] = ev.Message
	case "permission.asked":
		ev.Name, ev.ToolName = "PermissionRequest", d.Permission
		ev.Summary = strings.Join(d.Patterns, " ")
		payload["tool_name"] = d.Permission
		payload["tool_input"] = map[string]any{"patterns": d.Patterns}
	case "permission.replied":
		ev.Name = "PermissionReplied"
	case "form.created":
		ev.Name = "Question"
		if d.Form != nil {
			ev.Message = d.Form.Title
		}
		payload["message"] = ev.Message
	case "form.replied", "form.cancelled":
		ev.Name = "QuestionAnswered"
	case "session.deleted", "session.archived":
		ev.Name = "SessionEnd"
	case "session.renamed":
		return Result{Title: d.Title}
	case "rpc.opencode.goal.changed":
		res := Result{GoalSet: true}
		if g := d.Goal; g != nil {
			st := g.Status
			if st == "complete" {
				st = model.GoalMet
			}
			res.Goal = &model.Goal{Objective: g.Objective, Status: st, Reason: g.Reason, Rounds: g.Rounds, MaxRounds: g.MaxRounds, UpdatedAt: g.UpdatedAt}
		}
		return res
	case "session.usage.updated":
		return Result{Usage: &model.Usage{
			CostUSD: d.Cost, CostKnown: true,
			InputTokens: d.Tokens.Input, OutputTokens: d.Tokens.Output + d.Tokens.Reasoning,
			CacheReadTokens: d.Tokens.Cache.Read, CacheWriteTokens: d.Tokens.Cache.Write,
			UpdatedAt: time.Now().UnixMilli(),
		}}
	default:
		return Result{}
	}
	return Result{Event: withRaw(ev, payload)}
}

func withRaw(ev *model.Event, payload map[string]any) *model.Event {
	if len(payload) > 0 {
		ev.Raw, _ = json.Marshal(payload)
	}
	return ev
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
