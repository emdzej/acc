// Package hook implements `acc hook <agent>`: it is invoked by an agent's hook
// system, forwards the event to the daemon and prints any queued messages in
// the agent's injection format. It must never break the agent: every failure
// path spools the event and exits cleanly.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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

// harness adapts one agent's hook system: it turns the hook's stdin into
// an ACC event, and queued messages into what the hook prints to inject
// them. Adding an agent's hooks means adding an entry to harnesses.
type harness struct {
	// parse reads the hook input. It fills the event's fields except Agent,
	// Raw and TS, and sets Deliver on events whose output can inject messages.
	parse func(raw []byte) (model.Event, error)
	// output is what the hook prints for messages delivered with ev.
	output func(ev model.Event, msgs []model.Message) any
}

var harnesses = map[string]harness{
	AgentClaude: {parseClaude, func(ev model.Event, msgs []model.Message) any { return claudeOutput(ev.Name, msgs) }},
	// Codex takes the same injection output as Claude Code.
	AgentCodex:   {parseCodex, func(ev model.Event, msgs []model.Message) any { return claudeOutput(ev.Name, msgs) }},
	AgentCopilot: {parseCopilot, copilotOutput},
	AgentGemini:  {parseGemini, geminiOutput},
}

// errSkip: the hook event means nothing to ACC; the hook exits quietly.
var errSkip = errors.New("skip")

// LaunchEnv carries the ID ACC registered a session under when it started
// one for an agent that can't take a session ID (see agent.Agent.LaunchEnv).
const LaunchEnv = "ACC_LAUNCH_ID"

// Agents lists the agents `acc hook` handles.
func Agents() []string {
	out := make([]string, 0, len(harnesses))
	for a := range harnesses {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// Run handles one hook invocation. Errors are reported on stderr only; the
// caller should still exit 0.
func Run(agent string, in io.Reader, out io.Writer) error {
	h, ok := harnesses[agent]
	if !ok {
		return fmt.Errorf("unsupported agent %q", agent)
	}
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return err
	}
	ev, err := h.parse(raw)
	if errors.Is(err, errSkip) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("parse hook input: %w", err)
	}
	ev.Agent, ev.Raw, ev.TS = agent, raw, time.Now().UnixMilli()
	if id := os.Getenv(LaunchEnv); id != "" && id != ev.SessionID {
		ev.LaunchID = id
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
	return json.NewEncoder(out).Encode(h.output(ev, resp.Messages))
}

// parseClaude reads a Claude Code hook's input.
func parseClaude(raw []byte) (model.Event, error) {
	var ci claudeInput
	if err := json.Unmarshal(raw, &ci); err != nil {
		return model.Event{}, err
	}
	return model.Event{
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
		// Subagent hooks never deliver: output would go to the subagent's context.
		Deliver: claudeDeliverable[ci.HookEventName] && ci.AgentID == "",
	}, nil
}

// messagesText is how queued messages reach the model.
func messagesText(msgs []model.Message) string {
	var b strings.Builder
	b.WriteString("[ACC] The operator sent you the following message(s) via Agents Control Centre. Treat them as instructions from the user:\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "\n--- message %d from %s ---\n%s\n", m.ID, m.Sender, m.Body)
	}
	return b.String()
}

func claudeOutput(event string, msgs []model.Message) any {
	text := messagesText(msgs)
	if event == "Stop" {
		// Blocking the stop makes Claude continue its turn with the reason as input.
		return map[string]any{"decision": "block", "reason": text}
	}
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": text,
		},
	}
}

// spoolFile is where undeliverable events wait for the daemon: one JSON
// line per event. One file rather than one per event, because opening and
// deleting thousands of small files makes replay slow (seconds per thousand
// on a Mac with endpoint protection), and replay holds up the daemon's start.
const spoolFile = "events.jsonl"

// spool persists an undeliverable event; `acc serve` replays the spool on start.
// Spooled events never request delivery: queued messages would be lost.
func spool(ev model.Event, cause error) error {
	ev.Deliver = false
	dir := config.SpoolDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%v (spool failed: %w)", cause, err)
	}
	b, _ := json.Marshal(ev)
	if err := appendLocked(filepath.Join(dir, spoolFile), append(b, '\n')); err != nil {
		return fmt.Errorf("%v (spool failed: %w)", cause, err)
	}
	return nil // spooled successfully: not an error for the agent
}

// appendLocked appends line to path under an exclusive lock, so concurrent
// hooks don't interleave. Replay renames the file away before reading it;
// a writer that opened the old file retries on the new one.
func appendLocked(path string, line []byte) error {
	for range 5 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return err
		}
		held, err1 := f.Stat()
		cur, err2 := os.Stat(path)
		if err1 == nil && err2 == nil && os.SameFile(held, cur) {
			_, err = f.Write(line)
			f.Close() // releases the lock
			return err
		}
		f.Close() // renamed for replay meanwhile: append to the new file
	}
	return errors.New("spool file keeps moving")
}

// SpoolPending reports whether events wait in the spool.
func SpoolPending() bool {
	entries, _ := os.ReadDir(config.SpoolDir())
	return len(entries) > 0
}

// ReplaySpool feeds spooled events to fn in the order they were spooled,
// removing them once fn accepts them. If fn fails, the rest stay spooled.
func ReplaySpool(fn func(model.Event) error) (int, error) {
	dir := config.SpoolDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	n := 0
	// One file per event, as older versions spooled; names start with the timestamp.
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
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
	// Take the spool file over, so new events start a fresh one, then replay
	// what an interrupted replay left behind and the file just taken.
	if _, err := os.Stat(filepath.Join(dir, spoolFile)); err == nil {
		taken := filepath.Join(dir, fmt.Sprintf("replay-%020d.jsonl", time.Now().UnixNano()))
		if err := os.Rename(filepath.Join(dir, spoolFile), taken); err != nil {
			return n, err
		}
	}
	batches, _ := filepath.Glob(filepath.Join(dir, "replay-*.jsonl"))
	slices.Sort(batches)
	for _, path := range batches {
		m, err := replayFile(path, fn)
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// replayFile replays one taken spool file and removes it. On failure the
// events not yet accepted are written back, so they replay next time.
func replayFile(path string, fn func(model.Event) error) (int, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// Wait for a hook still writing to it from before the rename.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	lines := strings.SplitAfter(string(b), "\n")
	n := 0
	for i, line := range lines {
		var ev model.Event
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &ev) != nil {
			continue // a torn or blank line
		}
		if err := fn(ev); err != nil {
			rest := strings.Join(lines[i:], "")
			if werr := f.Truncate(0); werr == nil {
				f.WriteAt([]byte(rest), 0)
			}
			return n, err
		}
		n++
	}
	return n, os.Remove(path)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
