// Package hook implements `acc hook <agent>`: it is invoked by an agent's hook
// system, forwards the event to the daemon and prints any queued messages in
// the agent's injection format. It must never break the agent: every failure
// path spools the event and exits cleanly.
package hook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/model"
)

const (
	AgentClaude = "claude-code"
	postTimeout = 2 * time.Second
)

// claudeInput is the subset of Claude Code's hook stdin payload ACC uses.
type claudeInput struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	Cwd              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	ToolName         string `json:"tool_name"`
	Prompt           string `json:"prompt"`
	Message          string `json:"message"`
	Source           string `json:"source"`
	NotificationType string `json:"notification_type"`
	AgentID          string `json:"agent_id"`
	AgentType        string `json:"agent_type"`
	AgentTranscript  string `json:"agent_transcript_path"`
	LastAssistant    string `json:"last_assistant_message"`
	Reason           string `json:"reason"`
	ToolInput        struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		URL         string `json:"url"`
		Query       string `json:"query"`
		Prompt      string `json:"prompt"`
		Plan        string `json:"plan"`
		Questions   []struct {
			Question string `json:"question"`
		} `json:"questions"`
	} `json:"tool_input"`
}

// summary is a one-line, human-oriented description of the event.
func (ci claudeInput) summary() string {
	ti := ci.ToolInput
	switch ci.HookEventName {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest":
		if len(ti.Questions) > 0 {
			return ti.Questions[0].Question
		}
		return firstNonEmpty(ti.Command, ti.FilePath, ti.Plan, ti.Pattern, ti.URL, ti.Query, ti.Path, ti.Description, ti.Prompt)
	case "UserPromptSubmit":
		return ci.Prompt
	case "Notification":
		return ci.Message
	case "SessionStart":
		return ci.Source
	case "SessionEnd":
		return ci.Reason
	case "SubagentStart":
		return ci.AgentType
	}
	return ""
}

// claudeDeliverable lists events where injected text makes the agent act on it:
// hooks inside a running turn, or Stop, whose block continues the turn.
// SessionStart is deliberately absent: its context lands with no turn to
// follow (e.g. after /compact), so the agent would never act on the message.
var claudeDeliverable = map[string]bool{
	"UserPromptSubmit": true,
	"PostToolUse":      true,
	"Stop":             true,
}

// Run handles one hook invocation. Errors are reported on stderr only; the
// caller should still exit 0.
func Run(agent string, in io.Reader, out io.Writer) error {
	if agent != AgentClaude {
		return fmt.Errorf("unsupported agent %q", agent)
	}
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return err
	}
	var ci claudeInput
	if err := json.Unmarshal(raw, &ci); err != nil {
		return fmt.Errorf("parse hook input: %w", err)
	}
	ev := model.Event{
		Agent:               agent,
		SessionID:           ci.SessionID,
		Name:                ci.HookEventName,
		Cwd:                 ci.Cwd,
		TranscriptPath:      ci.TranscriptPath,
		ToolName:            ci.ToolName,
		Message:             firstNonEmpty(ci.Message, ci.Prompt, ci.LastAssistant),
		NotificationType:    ci.NotificationType,
		AgentID:             ci.AgentID,
		AgentType:           ci.AgentType,
		AgentTranscriptPath: ci.AgentTranscript,
		Summary:             strings.Join(strings.Fields(ci.summary()), " "),
		Raw:                 raw,
		TS:                  time.Now().UnixMilli(),
		// Subagent hooks never deliver: output would go to the subagent's context.
		Deliver: claudeDeliverable[ci.HookEventName] && ci.AgentID == "",
	}

	c, err := client.FromEnv(postTimeout)
	if err != nil {
		return spool(ev, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
	defer cancel()
	resp, err := c.PostEvent(ctx, ev)
	if err != nil {
		return spool(ev, err)
	}
	if len(resp.Messages) == 0 {
		return nil
	}
	return json.NewEncoder(out).Encode(claudeOutput(ev.Name, resp.Messages))
}

func claudeOutput(event string, msgs []model.Message) any {
	var b strings.Builder
	b.WriteString("[ACC] The operator sent you the following message(s) via Agents Control Centre. Treat them as instructions from the user:\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "\n--- message %d from %s ---\n%s\n", m.ID, m.Sender, m.Body)
	}
	if event == "Stop" {
		// Blocking the stop makes Claude continue its turn with the reason as input.
		return map[string]any{"decision": "block", "reason": b.String()}
	}
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": b.String(),
		},
	}
}

// spool persists an undeliverable event; `acc serve` replays the spool on start.
// Spooled events never request delivery: queued messages would be lost.
func spool(ev model.Event, cause error) error {
	ev.Deliver = false
	dir := config.SpoolDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%v (spool failed: %w)", cause, err)
	}
	b, _ := json.Marshal(ev)
	suffix := make([]byte, 4)
	rand.Read(suffix)
	name := filepath.Join(dir, fmt.Sprintf("%d-%s.json", ev.TS, hex.EncodeToString(suffix)))
	if err := os.WriteFile(name, b, 0o600); err != nil {
		return fmt.Errorf("%v (spool failed: %w)", cause, err)
	}
	return nil // spooled successfully: not an error for the agent
}

// ReplaySpool feeds spooled events to fn in timestamp order, deleting each on success.
func ReplaySpool(fn func(model.Event) error) (int, error) {
	entries, err := os.ReadDir(config.SpoolDir())
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries { // ReadDir sorts by name, and names start with the timestamp
		path := filepath.Join(config.SpoolDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ev model.Event
		if json.Unmarshal(b, &ev) != nil {
			os.Remove(path)
			continue
		}
		if err := fn(ev); err != nil {
			return n, err
		}
		os.Remove(path)
		n++
	}
	return n, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
