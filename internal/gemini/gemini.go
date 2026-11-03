// Package gemini supports Google's Gemini CLI. Gemini reports through hooks
// (see internal/hook); this package reads its chat recordings for what
// hooks don't carry: usage, and past sessions for History. It only reads
// Gemini's files.
package gemini

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

const Agent = "gemini"

// Dir is Gemini's runtime directory: $GEMINI_CLI_HOME/.gemini, else ~/.gemini.
func Dir() string {
	home := os.Getenv("GEMINI_CLI_HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".gemini")
}

// SettingsPath is the user settings file, where hooks are configured.
func SettingsPath() string { return filepath.Join(Dir(), "settings.json") }

// SessionFiles lists the chat recordings: <Dir>/tmp/<project>/chats/session-*.jsonl.
func SessionFiles() []string {
	files, _ := filepath.Glob(filepath.Join(Dir(), "tmp", "*", "chats", "session-*.jsonl"))
	return files
}

// ErrForbidden: the path isn't a Gemini chat recording.
var ErrForbidden = errors.New("not a Gemini chat recording")

// checkPath allows only .jsonl files under Gemini's tmp directory, since
// the path comes from a hook event.
func checkPath(path string) (string, error) {
	if !strings.HasSuffix(path, ".jsonl") {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(filepath.Join(Dir(), "tmp"))
	if err != nil {
		return "", ErrForbidden
	}
	if rel, err := filepath.Rel(root, real); err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrForbidden
	}
	return real, nil
}

// record is one line of a recording: the metadata, a message (re-appended
// whole when it changes, so the last copy of an id wins), or an update.
type record struct {
	SessionID string          `json:"sessionId"`
	StartTime string          `json:"startTime"`
	Summary   string          `json:"summary"`
	ID        string          `json:"id"`
	Type      string          `json:"type"` // user, gemini, info, error, warning
	Content   json.RawMessage `json:"content"`
	Model     string          `json:"model"`
	Tokens    *struct {
		Input    int64 `json:"input"` // includes cached input
		Output   int64 `json:"output"`
		Cached   int64 `json:"cached"`
		Thoughts int64 `json:"thoughts"`
	} `json:"tokens"`
	Set *struct {
		Summary string `json:"summary"`
	} `json:"$set"`
}

// recording is what a chat recording says once its updates are applied.
type recording struct {
	id, summary, prompt string
	started             int64
	usage               model.Usage
	model               string
}

func read(path string) (recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return recording{}, err
	}
	defer f.Close()
	var rec recording
	type msgTokens struct {
		in, out, cached int64
		model           string
	}
	byID := map[string]msgTokens{}
	var order []string
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		b, err := r.ReadBytes('\n')
		var x record
		if len(b) > 0 && json.Unmarshal(b, &x) == nil {
			switch {
			case x.SessionID != "" && rec.id == "":
				rec.id, rec.summary = x.SessionID, x.Summary
				if t, err := time.Parse(time.RFC3339Nano, x.StartTime); err == nil {
					rec.started = t.UnixMilli()
				}
			case x.Set != nil && x.Set.Summary != "":
				rec.summary = x.Set.Summary
			case x.Type == "user" && rec.prompt == "":
				// The first user message is Gemini's own session context;
				// tool results are user messages without text.
				if t := text(x.Content); !strings.HasPrefix(t, "<session_context>") {
					rec.prompt = t
				}
			case x.Type == "gemini" && x.ID != "" && x.Tokens != nil:
				if _, seen := byID[x.ID]; !seen {
					order = append(order, x.ID)
				}
				t := x.Tokens
				byID[x.ID] = msgTokens{t.Input, t.Output + t.Thoughts, t.Cached, x.Model}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return recording{}, err
		}
	}
	models := map[string]bool{}
	for _, id := range order {
		t := byID[id]
		rec.usage.InputTokens += t.in - t.cached
		rec.usage.CacheReadTokens += t.cached
		rec.usage.OutputTokens += t.out
		if t.model != "" {
			rec.model = t.model
			if !models[t.model] {
				models[t.model] = true
				rec.usage.Models = append(rec.usage.Models, t.model)
			}
		}
	}
	return rec, nil
}

// Usage is a session's tokens, summed over Gemini's responses. Gemini
// records no cost.
func Usage(path string) (*model.Usage, *model.StatusReport, error) {
	real, err := checkPath(path)
	if err != nil {
		return nil, nil, err
	}
	rec, err := read(real)
	if err != nil {
		return nil, nil, err
	}
	var st *model.StatusReport
	if rec.model != "" {
		st = &model.StatusReport{Agent: Agent, Model: rec.model}
	}
	return &rec.usage, st, nil
}

// ReadSession reads a chat recording for History. Its directory comes
// from the project's .project_root. ok is false for files that aren't a
// session.
func ReadSession(path string, fi os.FileInfo) (model.PastSession, bool) {
	rec, err := read(path)
	if err != nil || rec.id == "" {
		return model.PastSession{}, false
	}
	root, _ := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), ".project_root"))
	s := model.PastSession{Agent: Agent, ID: rec.id, Cwd: strings.TrimSpace(string(root)), TranscriptPath: path,
		Size: fi.Size(), StartedAt: rec.started, LastActive: fi.ModTime().UnixMilli()}
	if s.Cwd == "" || rec.prompt == "" && rec.summary == "" {
		return model.PastSession{}, false // no directory to resume in, or nothing said yet
	}
	s.Title = clip(strings.Join(strings.Fields(firstNonEmpty(rec.summary, rec.prompt)), " "), 80)
	u := rec.usage
	u.UpdatedAt = s.LastActive
	s.Usage = &u
	return s, true
}

// text is a message's text: a string, or its parts' text.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
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

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
