// Package pi supports pi (github.com/earendil-works/pi), the coding agent
// harness. ACC's extension (see internal/setup) forwards pi's events raw;
// Translate maps them to ACC's canonical events here, in Go, so the mapping
// is testable and can follow pi without reinstalling the extension. The
// package also reads pi's session files for History and usage.
package pi

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/model"
)

const Agent = "pi"

// RawEvent is one pi extension event as ACC's extension forwards it.
type RawEvent struct {
	Type    string  `json:"type"`
	TS      int64   `json:"ts"` // unix ms
	Session Session `json:"session"`
	Model   string  `json:"model,omitempty"` // the session's model, provider/id
	// Context is ctx.getContextUsage(), sent with turn ends.
	Context *struct {
		Tokens        *int64   `json:"tokens"`
		ContextWindow int64    `json:"contextWindow"`
		Percent       *float64 `json:"percent"`
	} `json:"context,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Session is the session the event belongs to (ctx.sessionManager).
type Session struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Cwd  string `json:"cwd"`
	Name string `json:"name,omitempty"`
}

// Result is what one event changes in ACC.
type Result struct {
	Event       *model.Event // a lifecycle event to record, if any
	Title       string       // the session's name (/name, --name)
	Model       string
	ContextPct  *float64
	ContextSize int64
}

type data struct {
	Reason   string          `json:"reason"`
	Name     string          `json:"name"`
	Text     string          `json:"text"`
	Source   string          `json:"source"`
	ToolName string          `json:"toolName"`
	Args     json.RawMessage `json:"args"`
	IsError  bool            `json:"isError"`
	Result   string          `json:"result"` // the tool result's text, clipped by the extension
	Kind     string          `json:"kind"`   // ui_prompt_start: select, confirm, input, editor, custom
	Title    string          `json:"title"`
	Aborted  bool            `json:"aborted"`
	LastText string          `json:"lastText"` // agent_settled: the run's last assistant text
}

// Translate maps one event. Unknown or uninteresting events give an empty Result.
func Translate(raw RawEvent) Result {
	s := raw.Session
	if s.ID == "" {
		return Result{}
	}
	var d data
	if len(raw.Data) > 0 && json.Unmarshal(raw.Data, &d) != nil {
		return Result{}
	}
	ts := raw.TS
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	res := Result{Title: s.Name, Model: raw.Model}
	if c := raw.Context; c != nil {
		res.ContextPct, res.ContextSize = c.Percent, c.ContextWindow
	}
	ev := &model.Event{Agent: Agent, SessionID: s.ID, Cwd: s.Cwd, TranscriptPath: s.File, TS: ts}
	// Raw is Claude-shaped so ACC's "latest activity" view reads every agent.
	payload := map[string]any{}

	switch raw.Type {
	case "session_start":
		ev.Name, ev.Summary = "SessionStart", d.Reason
	case "session_shutdown":
		if d.Reason == "reload" {
			return res // the same session carries on in a fresh runtime
		}
		ev.Name, ev.Message, ev.Summary = "SessionEnd", d.Reason, d.Reason
	case "session_info_changed":
		res.Title = d.Name
		return res
	case "input":
		ev.Name, ev.Message, ev.Summary = "UserPromptSubmit", d.Text, oneLine(d.Text)
		payload["prompt"] = d.Text
	case "tool_execution_start":
		ev.Name, ev.ToolName, ev.Summary = "PreToolUse", d.ToolName, oneLine(argSummary(d.Args))
		payload["tool_name"], payload["tool_input"] = d.ToolName, d.Args
	case "tool_execution_end":
		ev.Name, ev.ToolName, ev.Message = "PostToolUse", d.ToolName, d.Result
		if d.IsError {
			ev.Name = "PostToolUseFailure"
			payload["error"] = d.Result
		}
		payload["tool_name"], payload["tool_response"] = d.ToolName, d.Result
	case "ui_prompt_start":
		// pi asks nothing itself; extensions do (permission gates confirm
		// a tool call, others ask questions).
		// The alert shows the prompt's title.
		ev.Name, ev.Message = "Question", firstNonEmpty(d.Title, d.Kind)
		if d.Kind == "confirm" {
			ev.Name = "PermissionRequest"
		}
		payload["message"] = ev.Message
	case "ui_prompt_end":
		ev.Name = "QuestionAnswered"
	case "agent_settled":
		// The final idle signal: agent_end can be followed by retries and
		// queued messages.
		ev.Name, ev.Message = "Stop", d.LastText
		if d.Aborted {
			ev.Summary = "aborted"
		}
		payload["last_assistant_message"] = d.LastText
	case "model_select":
		return res
	default:
		return Result{}
	}
	if len(payload) > 0 {
		ev.Raw, _ = json.Marshal(payload)
	}
	res.Event = ev
	return res
}

// argSummary is a tool call's most telling argument: the command, path or pattern.
func argSummary(args json.RawMessage) string {
	var a map[string]any
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	for _, k := range []string{"command", "path", "file_path", "pattern", "url", "query", "prompt"} {
		if s, ok := a[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// ---- session files ----------------------------------------------------------

// Dir is pi's config directory: $PI_CODING_AGENT_DIR, else ~/.pi/agent.
func Dir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

// SessionsDir is where pi keeps sessions: $PI_CODING_AGENT_SESSION_DIR,
// else <Dir>/sessions.
func SessionsDir() string {
	if d := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); d != "" {
		return d
	}
	return filepath.Join(Dir(), "sessions")
}

// SessionFiles lists the session files under dir: <dir>/--<cwd>--/<ts>_<id>.jsonl,
// or <dir>/<ts>_<id>.jsonl when pi was given a session directory.
func SessionFiles(dir string) []string {
	flat, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	nested, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	return append(flat, nested...)
}

// ErrForbidden: the path isn't a pi session file.
var ErrForbidden = errors.New("not a pi session file")

// checkPath allows only .jsonl files inside pi's sessions directory, since
// the path comes from an event.
func checkPath(path string) (string, error) {
	if !strings.HasSuffix(path, ".jsonl") {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(SessionsDir())
	if err != nil {
		return "", ErrForbidden
	}
	if rel, err := filepath.Rel(root, real); err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrForbidden
	}
	return real, nil
}

// entry is the subset of a session file line ACC reads.
type entry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	Name      string `json:"name"` // session_info
	Model     string `json:"model"`
	Usage     *usage `json:"usage"` // usage entries
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Model   string          `json:"model"`
		Usage   *usage          `json:"usage"`
	} `json:"message"`
}

type usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"` // includes reasoning
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Cost       struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// scan reads every line of a session file.
func scan(path string, fn func(e entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var e entry
			if json.Unmarshal(line, &e) == nil {
				fn(e)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// totals adds an entry's usage to u. Every branch of the session tree
// counts: abandoned branches were paid for too.
func totals(u *model.Usage, models map[string]bool, e entry) {
	x, m := e.Usage, e.Model
	if e.Type == "message" && e.Message != nil && e.Message.Role == "assistant" {
		x, m = e.Message.Usage, e.Message.Model
	} else if e.Type != "usage" {
		return
	}
	if x == nil {
		return
	}
	u.InputTokens += x.Input
	u.OutputTokens += x.Output
	u.CacheReadTokens += x.CacheRead
	u.CacheWriteTokens += x.CacheWrite
	u.CostUSD += x.Cost.Total
	if m != "" && !models[m] {
		models[m] = true
		u.Models = append(u.Models, m)
	}
}

// Usage is a session's token and cost totals, which pi records per response.
func Usage(path string) (*model.Usage, error) {
	real, err := checkPath(path)
	if err != nil {
		return nil, err
	}
	u := &model.Usage{CostKnown: true}
	models := map[string]bool{}
	if err := scan(real, func(e entry) { totals(u, models, e) }); err != nil {
		return nil, err
	}
	return u, nil
}

// ReadSession reads a session file for History. ok is false for files
// that aren't a pi session.
func ReadSession(path string, fi os.FileInfo) (model.PastSession, bool) {
	s := model.PastSession{Agent: Agent, TranscriptPath: path, Size: fi.Size(), LastActive: fi.ModTime().UnixMilli()}
	u := &model.Usage{CostKnown: true}
	models := map[string]bool{}
	var prompt string
	first := true
	err := scan(path, func(e entry) {
		if first {
			first = false
			if e.Type != "session" {
				return
			}
			s.ID, s.Cwd = e.ID, e.Cwd
			if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				s.StartedAt = t.UnixMilli()
			}
			return
		}
		switch {
		case e.Type == "session_info":
			s.Title = e.Name // the latest name wins
		case prompt == "" && e.Type == "message" && e.Message != nil && e.Message.Role == "user":
			prompt = text(e.Message.Content)
		}
		totals(u, models, e)
	})
	if err != nil || s.ID == "" || s.Cwd == "" {
		return model.PastSession{}, false
	}
	if s.Title == "" {
		s.Title = clip(oneLine(prompt), 80)
	}
	u.UpdatedAt = s.LastActive
	s.Usage = u
	return s, true
}

// text is a message's text: a string, or its text blocks.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, " ")
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
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
