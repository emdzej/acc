// Package copilot supports GitHub Copilot CLI. Copilot reports through
// hooks (see internal/hook); this package reads its session state for what
// hooks don't carry: usage from a session's events.jsonl, and past sessions
// for History from their workspace.yaml. It only reads Copilot's files.
package copilot

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

const Agent = "copilot"

// Home is Copilot's home directory: $COPILOT_HOME, else ~/.copilot.
func Home() string {
	if d := os.Getenv("COPILOT_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".copilot")
}

// SessionDir is where Copilot keeps a session's state.
func SessionDir(id string) string { return filepath.Join(Home(), "session-state", id) }

// EventsPath is a session's event log, which Copilot's Stop hook also names.
func EventsPath(id string) string { return filepath.Join(SessionDir(id), "events.jsonl") }

// ErrForbidden: the path isn't a Copilot session's event log.
var ErrForbidden = errors.New("not a Copilot session event log")

// checkPath allows only events.jsonl files under Copilot's session state,
// since the path comes from a hook event.
func checkPath(path string) (string, error) {
	if filepath.Base(path) != "events.jsonl" {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(filepath.Join(Home(), "session-state"))
	if err != nil {
		return "", ErrForbidden
	}
	if rel, err := filepath.Rel(root, real); err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrForbidden
	}
	return real, nil
}

// event is one line of events.jsonl: {"type", "id", "timestamp", "data"}.
type event struct {
	Type string `json:"type"`
	Data struct {
		Usage *struct {
			Model            string  `json:"model"`
			InputTokens      int64   `json:"inputTokens"`
			OutputTokens     int64   `json:"outputTokens"`
			CacheReadTokens  int64   `json:"cacheReadTokens"`
			CacheWriteTokens int64   `json:"cacheWriteTokens"`
			Cost             float64 `json:"cost"`
		} `json:"usage"`
		NewModel      string `json:"newModel"`
		SelectedModel string `json:"selectedModel"`
	} `json:"data"`
}

// Usage adds up a session's model calls (session.usage_record, one per
// request). Copilot bills in premium requests, not dollars; its records'
// cost is reported as is, and only counts when non-zero.
func Usage(path string) (*model.Usage, *model.StatusReport, error) {
	real, err := checkPath(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	u := &model.Usage{}
	st := &model.StatusReport{Agent: Agent}
	models := map[string]bool{}
	addModel := func(m string) {
		if m != "" && !models[m] {
			models[m] = true
			u.Models = append(u.Models, m)
		}
	}
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		b, err := r.ReadBytes('\n')
		var e event
		if len(b) > 0 && json.Unmarshal(b, &e) == nil {
			switch e.Type {
			case "session.usage_record":
				if x := e.Data.Usage; x != nil {
					u.InputTokens += x.InputTokens
					u.OutputTokens += x.OutputTokens
					u.CacheReadTokens += x.CacheReadTokens
					u.CacheWriteTokens += x.CacheWriteTokens
					u.CostUSD += x.Cost
					addModel(x.Model)
					st.Model = x.Model
				}
			case "session.start":
				st.Model = e.Data.SelectedModel
			case "session.model_change":
				st.Model = e.Data.NewModel
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
	}
	u.CostKnown = u.CostUSD > 0
	if st.Model == "" {
		st = nil
	}
	return u, st, nil
}

// LastReply is the turn's final assistant text, from the end of the
// session's event log: Copilot's Stop hook doesn't carry it. Copilot can
// run the hook before the reply reaches the log, so it waits up to wait
// for a reply newer than the last user message.
func LastReply(path string, wait time.Duration) string {
	real, err := checkPath(path)
	if err != nil {
		return ""
	}
	deadline := time.Now().Add(wait)
	for {
		reply, current := lastReply(real)
		if current || !time.Now().Before(deadline) {
			return reply
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// lastReply scans the log's tail backwards; current is false while the
// latest user message has no reply after it yet.
func lastReply(path string) (reply string, current bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", true
	}
	defer f.Close()
	const tail = 256 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		f.Seek(fi.Size()-tail, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], `"type":"user.message"`) {
			return "", false
		}
		if !strings.Contains(lines[i], `"assistant.message"`) {
			continue
		}
		var e struct {
			Type string `json:"type"`
			Data struct {
				Content string `json:"content"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(lines[i]), &e) == nil && e.Type == "assistant.message" && strings.TrimSpace(e.Data.Content) != "" {
			return e.Data.Content, true
		}
	}
	return "", true
}

// Workspaces lists the sessions' workspace.yaml files, for History.
func Workspaces() []string {
	files, _ := filepath.Glob(filepath.Join(Home(), "session-state", "*", "workspace.yaml"))
	return files
}

// ReadWorkspace reads a session's workspace.yaml (flat "key: value" lines)
// for History. ok is false for files that aren't a session's.
func ReadWorkspace(path string, fi os.FileInfo) (model.PastSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return model.PastSession{}, false
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "-") {
			continue // nested values aren't needed
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			kv[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
		}
	}
	s := model.PastSession{Agent: Agent, ID: kv["id"], Cwd: kv["cwd"], Title: firstNonEmpty(kv["name"], kv["summary"]),
		StartedAt: parseTime(kv["created_at"]), LastActive: fi.ModTime().UnixMilli()}
	if s.ID == "" || s.ID != filepath.Base(filepath.Dir(path)) || s.Cwd == "" {
		return model.PastSession{}, false
	}
	if t := parseTime(kv["updated_at"]); t > 0 {
		s.LastActive = t
	}
	s.TranscriptPath = filepath.Join(filepath.Dir(path), "events.jsonl")
	if efi, err := os.Stat(s.TranscriptPath); err == nil {
		s.Size = efi.Size()
		s.LastActive = max(s.LastActive, efi.ModTime().UnixMilli())
	}
	if u, _, err := Usage(s.TranscriptPath); err == nil {
		u.UpdatedAt = s.LastActive
		s.Usage = u
	}
	s.Title = clip(strings.Join(strings.Fields(s.Title), " "), 80)
	return s, true
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		if v[0] == '"' {
			var s string
			if json.Unmarshal([]byte(v), &s) == nil {
				return s
			}
		}
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

func parseTime(s string) int64 {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UnixMilli()
	}
	return 0
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
