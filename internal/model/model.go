// Package model holds the wire and storage types shared by the daemon and its clients.
package model

import (
	"encoding/json"
	"strings"
)

// Session statuses, derived from the most recent lifecycle event.
const (
	StatusActive  = "active"  // started, no prompt yet
	StatusWorking = "working" // processing a prompt or running tools
	StatusWaiting = "waiting" // needs user attention (permission prompt, idle notification)
	StatusIdle    = "idle"    // finished its turn
	StatusEnded   = "ended"
	// StatusStarting: launched from ACC, the agent hasn't reported in yet.
	StatusStarting = "starting"
)

// Event is a normalized agent lifecycle event, posted by `acc hook`.
type Event struct {
	Agent          string `json:"agent"`
	SessionID      string `json:"session_id"`
	Name           string `json:"name"`
	Cwd            string `json:"cwd,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	Message        string `json:"message,omitempty"`
	// AgentID/AgentType are set when the event comes from a subagent of SessionID.
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	// NotificationType is Claude Code's notification kind (permission_prompt, idle_prompt…).
	NotificationType string `json:"notification_type,omitempty"`
	// AgentTranscriptPath is the subagent's own transcript (SubagentStop).
	AgentTranscriptPath string `json:"agent_transcript_path,omitempty"`
	// Summary is a one-line description for activity logs (command, file, prompt…).
	Summary string          `json:"summary,omitempty"`
	Raw     json.RawMessage `json:"raw,omitempty"`
	TS      int64           `json:"ts"`
	// Deliver asks the daemon to hand back queued messages, because the hook
	// that sent this event can inject them into the session.
	Deliver bool `json:"deliver,omitempty"`
}

// EventResponse carries messages to be injected into the session.
type EventResponse struct {
	Messages []Message `json:"messages,omitempty"`
}

type Session struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	// Title is what to display: the /rename title, else the agent's
	// auto-generated title, else the working directory's name.
	Title          string  `json:"title"`
	ACCTitle       string  `json:"acc_title,omitempty"` // set in ACC; wins over the agent's titles
	CustomTitle    string  `json:"custom_title,omitempty"`
	AITitle        string  `json:"ai_title,omitempty"`
	DirTitle       string  `json:"dir_title,omitempty"`
	Color          string  `json:"color,omitempty"` // own colour; overrides group colour
	Status         string  `json:"status"`
	LastEvent      string  `json:"last_event"`
	LastTool       string  `json:"last_tool,omitempty"`
	LastMessage    string  `json:"last_message,omitempty"`
	TranscriptPath string  `json:"transcript_path,omitempty"`
	StartedAt      int64   `json:"started_at"`
	LastSeen       int64   `json:"last_seen"`
	Groups         []Group `json:"groups"`
	PendingCount   int     `json:"pending_count"`
	// Channel is true while the session has a live ACC channel (acc mcp)
	// that delivers messages immediately, waking the session if it is idle.
	Channel bool `json:"channel,omitempty"`
	// Host is where ACC started the session (tmux or Ghostty), if it did:
	// such sessions accept slash commands and can be brought back on screen.
	Host *Host `json:"host,omitempty"`
	// Attention says the agent is waiting on the user: AttentionPermission,
	// AttentionQuestion or AttentionInput ("" = not waiting on you).
	Attention       string `json:"attention,omitempty"`
	AttentionDetail string `json:"attention_detail,omitempty"`
	AttentionSince  int64  `json:"attention_since,omitempty"`
	// Model and context-window fill, from the agent's status line feed.
	Model       string   `json:"model,omitempty"`
	ContextPct  *float64 `json:"context_pct,omitempty"`
	ContextSize int64    `json:"context_size,omitempty"`
	// Usage is token/cost usage from the agent's own records; nil until known.
	Usage *Usage `json:"usage,omitempty"`
	Goal  *Goal  `json:"goal,omitempty"` // set with the agent's /goal
	// Subagents are the session's most recent subagents, newest first.
	Subagents []Subagent `json:"subagents"`
}

// Usage is an agent session's token and cost totals.
type Usage struct {
	CostUSD          float64  `json:"cost_usd"`
	CostKnown        bool     `json:"cost_known"` // false: tokens only, or some model had no price
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	Models           []string `json:"models,omitempty"`
	APIDurationMs    int64    `json:"api_duration_ms,omitempty"`
	DurationMs       int64    `json:"duration_ms,omitempty"`
	LinesAdded       int64    `json:"lines_added,omitempty"`
	LinesRemoved     int64    `json:"lines_removed,omitempty"`
	UpdatedAt        int64    `json:"updated_at"`
}

// PlanWindow is one subscription rate-limit window (e.g. 5-hour, weekly).
type PlanWindow struct {
	Key      string  `json:"key"`   // agent's name for the window, e.g. five_hour
	Label    string  `json:"label"` // short display label, e.g. 5h
	UsedPct  float64 `json:"used_pct"`
	ResetsAt int64   `json:"resets_at,omitempty"` // unix ms
	// Forecast, computed by the daemon (never reported by agents):
	// usage at reset at the current pace, and when it would hit 100%.
	ProjectedPct *float64 `json:"projected_pct,omitempty"`
	LimitAt      int64    `json:"limit_at,omitempty"` // unix ms; 0 = not before reset
	Pace         string   `json:"pace,omitempty"`     // PaceRecent | PaceWindow
}

// Pace bases for a forecast.
const (
	PaceRecent = "recent" // rate over the last stretch (45 min for 5h, 6 h for weekly)
	PaceWindow = "window" // average since the window opened (too little history)
)

// LimitWarning reports whether a window is forecast to run out before it resets.
func (w PlanWindow) LimitWarning() bool {
	return w.LimitAt > 0 && (w.ResetsAt == 0 || w.LimitAt < w.ResetsAt)
}

// PlanUsage is an agent account's plan limits, as last reported.
type PlanUsage struct {
	Agent     string       `json:"agent"`
	Windows   []PlanWindow `json:"windows"`
	UpdatedAt int64        `json:"updated_at"`
}

// StatusReport is what `acc statusline` forwards from an agent's status line feed.
type StatusReport struct {
	Agent       string       `json:"agent"`
	SessionID   string       `json:"session_id"`
	Model       string       `json:"model,omitempty"`
	ContextPct  *float64     `json:"context_pct,omitempty"`
	ContextSize int64        `json:"context_size,omitempty"`
	Plan        []PlanWindow `json:"plan,omitempty"`
}

// Attention states, most urgent first.
const (
	AttentionPermission = "permission" // a tool call needs approval
	AttentionQuestion   = "question"   // the agent asked something (AskUserQuestion, plan approval)
	AttentionNotice     = "notice"     // any other agent notification asking for the user
	AttentionInput      = "input"      // turn finished; waiting for the next prompt
)

// Subagent statuses.
const (
	SubagentRunning = "running"
	SubagentDone    = "done"
)

// Subagent is a child agent spawned by a main session (e.g. Claude Code's Task tool).
type Subagent struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	LastTool       string `json:"last_tool,omitempty"`
	Summary        string `json:"summary,omitempty"` // last action
	Result         string `json:"result,omitempty"`  // final message, once done
	TranscriptPath string `json:"transcript_path,omitempty"`
	StartedAt      int64  `json:"started_at"`
	LastSeen       int64  `json:"last_seen"`
}

// EventSummary is an activity-log entry for a session.
type EventSummary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	AgentID  string `json:"agent_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
	Summary  string `json:"summary,omitempty"`
	TS       int64  `json:"ts"`
}

// DisplayColor is the session's own colour, else its first group's, else "".
func (s Session) DisplayColor() string {
	if s.Color != "" {
		return s.Color
	}
	for _, g := range s.Groups {
		if g.Display != "" {
			return g.Display
		}
	}
	return ""
}

// SessionPatch updates mutable session fields. Color "" clears the override.
type SessionPatch struct {
	Color *string `json:"color,omitempty"`
	Title *string `json:"title,omitempty"` // ACC title; "" clears
}

// Group is a node in the group tree. Groups are addressed by Path ("a/b/c").
type Group struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parent_id"` // 0 = root
	Name     string `json:"name"`
	Path     string `json:"path"`
	Depth    int    `json:"depth"`
	Color    string `json:"color"`         // own colour; "" inherits from the parent
	Display  string `json:"display_color"` // own colour, else nearest ancestor's, else ""
}

type Message struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	Body        string `json:"body"`
	Sender      string `json:"sender"`
	CreatedAt   int64  `json:"created_at"`
	DeliveredAt int64  `json:"delivered_at,omitempty"`
}

// SendRequest targets either one session or every live session in a group.
type SendRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Group     string `json:"group,omitempty"`
	Body      string `json:"body"`
}

type SendResponse struct {
	Queued []Message `json:"queued"`
	// Typed lists sessions a slash command was typed into; Skipped maps
	// sessions it couldn't reach to the reason.
	Typed   []string          `json:"typed,omitempty"`
	Skipped map[string]string `json:"skipped,omitempty"`
}

type GroupRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type AssignRequest struct {
	SessionID string `json:"session_id"`
	Group     string `json:"group"`
	Remove    bool   `json:"remove,omitempty"`
}

// Host is a session's terminal: a tmux session name or a Ghostty terminal ID.
type Host struct {
	Kind string `json:"kind"` // tmux | ghostty
	Ref  string `json:"ref"`
}

// TypeRequest types text into a session's terminal and presses Enter.
type TypeRequest struct {
	Text string `json:"text"`
	// Force types even while the session shows a permission or question dialog.
	Force bool `json:"force,omitempty"`
}

// LaunchRequest starts a new agent session in a terminal window.
type LaunchRequest struct {
	Cwd    string `json:"cwd"`
	Title  string `json:"title,omitempty"`  // ACC title, also the agent's session name
	Group  string `json:"group,omitempty"`  // group path to put the session in
	Color  string `json:"color,omitempty"`  // session colour
	Prompt string `json:"prompt,omitempty"` // first prompt
	Tmux   *bool  `json:"tmux,omitempty"`   // host in tmux; nil = config default
}

type LaunchResponse struct {
	SessionID string `json:"session_id"`
	Terminal  string `json:"terminal"`
	Command   string `json:"command"`
	Host      *Host  `json:"host,omitempty"`
}

// Goal statuses.
const (
	GoalActive = "active"
	GoalPaused = "paused"
	GoalMet    = "met"
	GoalFailed = "failed"
)

// Goal is the objective a session keeps working towards (/goal in Claude
// Code and OpenCode): the agent is re-prompted until a judge says it's met.
type Goal struct {
	Objective string `json:"objective"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"` // the judge's latest verdict, or why it stopped
	Rounds    int    `json:"rounds,omitempty"`
	MaxRounds int    `json:"max_rounds,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// ShortID is a session id prefix for display and for commands that accept
// one. OpenCode ids (ses_ + a time-ordered part) share a long prefix, so
// they keep more characters.
func ShortID(id string) string {
	n := 8
	if strings.HasPrefix(id, "ses_") {
		n = 16
	}
	return id[:min(n, len(id))]
}
