// Package transcript reads agent transcripts incrementally and extracts the
// "latest activity" view from hook payloads. Claude Code only, for now.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxText   = 4000      // per entry
	tailBytes = 256 << 10 // initial window when following from the end
	maxRead   = 1 << 20   // per request
)

// Entry kinds.
const (
	KindPrompt     = "prompt"
	KindText       = "text"
	KindToolUse    = "tool_use"
	KindToolResult = "tool_result"
	KindThinking   = "thinking"
)

type Entry struct {
	TS    int64  `json:"ts"`
	Role  string `json:"role"`
	Kind  string `json:"kind"`
	Tool  string `json:"tool,omitempty"`
	Text  string `json:"text"`
	Error bool   `json:"error,omitempty"`
	// Source names the channel a prompt arrived through ("acc"); Text is then
	// the message body without the <channel> wrapper.
	Source string `json:"source,omitempty"`
}

// Page is a chunk of transcript; pass Offset back to continue following.
type Page struct {
	Entries []Entry `json:"entries"`
	Offset  int64   `json:"offset"`
	Path    string  `json:"path"`
}

var ErrForbidden = errors.New("transcript path outside allowed roots")

// AllowedRoots are the directories transcripts may be read from. Paths come
// from hook payloads, so this is what stops the API becoming an arbitrary file reader.
func AllowedRoots() []string {
	home, _ := os.UserHomeDir()
	roots := []string{filepath.Join(home, ".claude", "projects")}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		roots = append(roots, filepath.Join(d, "projects"))
	}
	return roots
}

func checkPath(path string) (string, error) {
	if !strings.HasSuffix(path, ".jsonl") {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, root := range AllowedRoots() {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(r, real); err == nil && !strings.HasPrefix(rel, "..") {
			return real, nil
		}
	}
	return "", ErrForbidden
}

// Read parses complete lines from offset. offset < 0 starts near the end of
// the file so following a long session doesn't replay all of it.
func Read(path string, offset int64) (Page, error) {
	real, err := checkPath(path)
	if err != nil {
		return Page{}, err
	}
	f, err := os.Open(real)
	if err != nil {
		return Page{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Page{}, err
	}
	skipPartial := false
	if offset < 0 {
		offset = max(0, fi.Size()-tailBytes)
		skipPartial = offset > 0
	}
	if offset > fi.Size() { // file was truncated or replaced
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return Page{}, err
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxRead))
	if err != nil {
		return Page{}, err
	}
	if skipPartial {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return Page{Entries: []Entry{}, Offset: offset, Path: path}, nil
		}
		buf, offset = buf[i+1:], offset+int64(i+1)
	}
	// Only consume complete lines; the agent may be mid-write.
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		return Page{Entries: []Entry{}, Offset: offset, Path: path}, nil
	}
	entries := []Entry{}
	sc := bufio.NewScanner(bytes.NewReader(buf[:end+1]))
	sc.Buffer(make([]byte, 0, 64<<10), maxRead)
	for sc.Scan() {
		entries = append(entries, parseLine(sc.Bytes())...)
	}
	return Page{Entries: entries, Offset: offset + int64(end+1), Path: path}, nil
}

type line struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}

func parseLine(b []byte) []Entry {
	var l line
	if json.Unmarshal(b, &l) != nil || (l.Type != "user" && l.Type != "assistant") {
		return nil // bookkeeping lines: attachments, cost state, queue ops…
	}
	var ts int64
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		ts = t.UnixMilli()
	}
	role := l.Message.Role
	var s string
	if json.Unmarshal(l.Message.Content, &s) == nil {
		src, body := Unwrap(s)
		return []Entry{{TS: ts, Role: role, Kind: KindPrompt, Text: clip(body), Source: src}}
	}
	var blocks []block
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return nil
	}
	var out []Entry
	for _, bl := range blocks {
		e := Entry{TS: ts, Role: role}
		switch bl.Type {
		case "text":
			e.Kind, e.Text = KindText, bl.Text
			if role == "user" {
				e.Kind = KindPrompt
				e.Source, e.Text = Unwrap(bl.Text)
			}
		case "thinking":
			if bl.Thinking == "" {
				continue // redacted thinking carries nothing to show
			}
			e.Kind, e.Text = KindThinking, bl.Thinking
		case "tool_use":
			e.Kind, e.Tool, e.Text = KindToolUse, bl.Name, ToolInputSummary(bl.Input)
		case "tool_result":
			e.Kind, e.Text, e.Error = KindToolResult, contentText(bl.Content), bl.IsError
		default:
			continue
		}
		e.Text = clip(e.Text)
		out = append(out, e)
	}
	return out
}

// channelRe matches a channel event as Claude Code wraps it:
// <channel source="acc" k="v"…>\nbody\n</channel>.
var channelRe = regexp.MustCompile(`(?s)^\s*<channel source="([^"]*)"[^>]*>\n?(.*?)\n?</channel>\s*$`)

// Unwrap strips the <channel> wrapper from a prompt delivered over a channel,
// returning the channel's source and the body; other prompts pass through.
func Unwrap(s string) (source, body string) {
	if m := channelRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2]
	}
	return "", s
}

// contentText flattens a tool_result content (string or text blocks).
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		} else if b.Type != "" {
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// ToolInputSummary renders a tool input as its most telling field, falling
// back to compact JSON.
func ToolInputSummary(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return string(raw)
	}
	for _, k := range []string{"command", "file_path", "pattern", "url", "query", "path", "description", "prompt"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	var buf bytes.Buffer
	json.Compact(&buf, raw)
	return buf.String()
}

func clip(s string) string {
	if len(s) <= maxText {
		return s
	}
	return strings.ToValidUTF8(s[:maxText], "") + fmt.Sprintf("… [%d more bytes]", len(s)-maxText)
}

// Latest is the default, transcript-free view of what a session did last.
type Latest struct {
	Event  string `json:"event"`
	Kind   string `json:"kind"` // tool | message | prompt | notification
	Tool   string `json:"tool,omitempty"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Text   string `json:"text,omitempty"`
	Error  bool   `json:"error,omitempty"`
	Source string `json:"source,omitempty"` // channel a prompt came through
	TS     int64  `json:"ts"`
}

// LatestEvents are the hook events FromPayload understands, most useful first.
var LatestEvents = []string{"PostToolUse", "PostToolUseFailure", "PreToolUse", "Stop", "SubagentStop", "UserPromptSubmit", "Notification", "PermissionRequest"}

// FromPayload builds a Latest from a Claude Code hook payload.
func FromPayload(event string, ts int64, payload []byte) Latest {
	var p struct {
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
		ToolResponse  json.RawMessage `json:"tool_response"`
		Error         string          `json:"error"`
		LastAssistant string          `json:"last_assistant_message"`
		Prompt        string          `json:"prompt"`
		Message       string          `json:"message"`
	}
	json.Unmarshal(payload, &p)
	l := Latest{Event: event, TS: ts}
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest":
		l.Kind, l.Tool, l.Input = "tool", p.ToolName, clip(ToolInputSummary(p.ToolInput))
		l.Output = clip(responseText(p.ToolResponse))
		if event == "PostToolUseFailure" {
			l.Error, l.Output = true, clip(firstNonEmpty(p.Error, l.Output))
		}
	case "Stop", "SubagentStop":
		l.Kind, l.Text = "message", clip(p.LastAssistant)
	case "UserPromptSubmit":
		src, body := Unwrap(p.Prompt)
		l.Kind, l.Text, l.Source = "prompt", clip(body), src
	case "Notification":
		l.Kind, l.Text = "notification", clip(p.Message)
	}
	return l
}

// responseText extracts readable output from a tool_response of any shape.
func responseText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var r struct {
		Stdout  *string         `json:"stdout"`
		Stderr  string          `json:"stderr"`
		Content json.RawMessage `json:"content"`
		File    struct {
			Content string `json:"content"`
		} `json:"file"`
	}
	if json.Unmarshal(raw, &r) == nil {
		if r.Stdout != nil { // shell-like tool: show its streams, even if empty
			return strings.Trim(*r.Stdout+"\n"+r.Stderr, "\n")
		}
		if r.File.Content != "" {
			return r.File.Content
		}
		if t := contentText(r.Content); t != "" {
			return t
		}
	}
	if t := contentText(raw); t != "" {
		return t
	}
	var buf bytes.Buffer
	json.Compact(&buf, raw)
	return buf.String()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
