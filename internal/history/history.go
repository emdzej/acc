// Package history finds the sessions agents keep on record, whether or not
// ACC has seen them: Claude Code's transcripts and OpenCode's session
// database. It only reads; nothing here changes an agent's files.
package history

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/pi"
	"github.com/emdzej/acc/internal/transcript"
)

const headBytes = 256 << 10 // where Claude Code transcripts record the directory and first prompt

// ClaudeConfigDirs are the Claude Code config directories ACC reads
// transcripts from: ~/.claude and $CLAUDE_CONFIG_DIR.
func ClaudeConfigDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".claude")}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" && d != dirs[0] {
		dirs = append(dirs, d)
	}
	return dirs
}

// ConfigDirOf is the Claude Code config directory a transcript lives in
// (<config>/projects/<project>/<id>.jsonl), or "".
func ConfigDirOf(transcriptPath string) string {
	d := filepath.Dir(filepath.Dir(transcriptPath))
	if filepath.Base(d) != "projects" {
		return ""
	}
	return filepath.Dir(d)
}

// Source lists one agent's past sessions, newest first. Sources that read
// one file per session should go through (*Index).File, which caches.
type Source struct {
	Agent string
	List  func(ctx context.Context, x *Index, limit int) ([]model.PastSession, error)
}

// Sources are the agents' records ACC reads. Adding an agent's history
// means adding its Source here.
var Sources = []Source{
	{"claude-code", func(_ context.Context, x *Index, _ int) ([]model.PastSession, error) {
		return x.Claude(ClaudeConfigDirs()...), nil
	}},
	{"opencode", func(ctx context.Context, _ *Index, limit int) ([]model.PastSession, error) {
		return OpenCode(ctx, OpenCodeDB(), limit)
	}},
	{pi.Agent, func(_ context.Context, x *Index, _ int) ([]model.PastSession, error) {
		var out []model.PastSession
		for _, f := range pi.SessionFiles(pi.SessionsDir()) {
			if s, ok := x.File(f, pi.ReadSession); ok {
				out = append(out, s)
			}
		}
		return out, nil
	}},
}

// All lists every source's sessions, newest first, capped at limit. A
// source that fails is skipped; its error is returned alongside.
func (x *Index) All(ctx context.Context, limit int) ([]model.PastSession, error) {
	var lists [][]model.PastSession
	var errs []error
	for _, src := range Sources {
		l, err := src.List(ctx, x, limit)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.Agent, err))
		}
		lists = append(lists, l)
	}
	return Merge(limit, lists...), errors.Join(errs...)
}

// Index lists past sessions, caching what it read from each transcript
// until the file changes.
type Index struct {
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	size  int64
	mtime time.Time
	s     model.PastSession
	ok    bool // a real session (has a directory), not e.g. an empty stub
}

func NewIndex() *Index { return &Index{cache: map[string]cached{}} }

// Claude lists the sessions in Claude Code's config directories, newest first.
func (x *Index) Claude(configDirs ...string) []model.PastSession {
	running := map[string]int{}
	var out []model.PastSession
	seen := map[string]bool{}
	for _, dir := range configDirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		for id, pid := range Running(dir) {
			running[id] = pid
		}
		files, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "*.jsonl"))
		for _, f := range files {
			if s, ok := x.claudeFile(f); ok {
				out = append(out, s)
			}
		}
	}
	for i := range out {
		_, out[i].Running = running[out[i].ID]
	}
	sortNewest(out)
	return out
}

// File reads one session file with parse, or returns what it read last
// time if the file hasn't changed since. parse reports false for files
// that aren't a usable session (no directory, an empty stub).
func (x *Index) File(path string, parse func(path string, fi os.FileInfo) (model.PastSession, bool)) (model.PastSession, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return model.PastSession{}, false
	}
	x.mu.Lock()
	c, hit := x.cache[path]
	x.mu.Unlock()
	if hit && c.size == fi.Size() && c.mtime.Equal(fi.ModTime()) {
		return c.s, c.ok
	}
	s, ok := parse(path, fi)
	x.mu.Lock()
	x.cache[path] = cached{fi.Size(), fi.ModTime(), s, ok}
	x.mu.Unlock()
	return s, ok
}

func (x *Index) claudeFile(path string) (model.PastSession, bool) { return x.File(path, claudeFile) }

func claudeFile(path string, fi os.FileInfo) (model.PastSession, bool) {
	s := model.PastSession{
		Agent: "claude-code", ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		TranscriptPath: path, Size: fi.Size(), LastActive: fi.ModTime().UnixMilli(),
	}
	var prompt string
	s.Cwd, s.StartedAt, prompt = readHead(path)
	if m, err := transcript.ReadMeta(path, false); err == nil {
		s.Title = cmp.Or(m.Custom, m.AI)
	}
	if s.Title == "" {
		s.Title = prompt
	}
	return s, s.Cwd != ""
}

// readHead finds the session's directory, start time and first typed prompt.
func readHead(path string) (cwd string, started int64, prompt string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, headBytes))
	sc.Buffer(make([]byte, 0, 64<<10), headBytes)
	for sc.Scan() {
		b := sc.Bytes()
		if !bytes.Contains(b, []byte(`"cwd"`)) {
			continue
		}
		var l struct {
			Type      string `json:"type"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
			IsMeta    bool   `json:"isMeta"`
			Message   struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(b, &l) != nil {
			continue
		}
		if cwd == "" && l.Cwd != "" {
			cwd = l.Cwd
			if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
				started = t.UnixMilli()
			}
		}
		if prompt == "" && l.Type == "user" && !l.IsMeta {
			prompt = promptText(l.Message.Content)
		}
		if cwd != "" && prompt != "" {
			return
		}
	}
	return
}

// promptText is a user message's text, if the user typed it (not a command
// wrapper or tool result), as one short line.
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var parts []struct {
			Type, Text string
		}
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type == "text" {
				s = p.Text
				break
			}
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || strings.HasPrefix(s, "<") {
		return ""
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

// Running maps the Claude Code sessions whose process is alive to their
// pid, from the registry Claude Code keeps in <config>/sessions/<pid>.json.
// It returns nil when there is no registry (older Claude Code).
func Running(configDir string) map[string]int {
	if !HasRegistry(configDir) {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(configDir, "sessions", "*.json"))
	out := map[string]int{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(b, &r) != nil || r.PID <= 0 || r.SessionID == "" {
			continue
		}
		// Signal 0 checks the process exists; EPERM means it does but isn't ours.
		if err := syscall.Kill(r.PID, 0); err == nil || errors.Is(err, syscall.EPERM) {
			out[r.SessionID] = r.PID
		}
	}
	return out
}

// HasRegistry reports whether this Claude Code keeps a registry of running
// sessions, so a session missing from it is known not to be running.
func HasRegistry(configDir string) bool {
	fi, err := os.Stat(filepath.Join(configDir, "sessions"))
	return err == nil && fi.IsDir()
}

// OpenCodeDB is OpenCode's session database.
func OpenCodeDB() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "opencode", "opencode.db")
}

// OpenCode lists OpenCode's top-level sessions, newest first. It reads the
// database read-only; a missing or unfamiliar database yields nothing.
func OpenCode(ctx context.Context, dbPath string, limit int) ([]model.PastSession, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []model.PastSession
	var lastErr error
	// OpenCode 2.x keeps sessions in session_v2; earlier versions in session.
	for _, table := range []string{"session_v2", "session"} {
		rows, err := db.QueryContext(ctx, `
SELECT id, directory, COALESCE(title, ''), time_created, time_updated, cost,
       tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write
FROM `+table+` WHERE parent_id IS NULL ORDER BY time_updated DESC LIMIT ?`, limit)
		if err != nil {
			lastErr = err
			continue
		}
		for rows.Next() {
			s := model.PastSession{Agent: "opencode"}
			var u model.Usage
			var reasoning int64
			if err := rows.Scan(&s.ID, &s.Cwd, &s.Title, &s.StartedAt, &s.LastActive, &u.CostUSD,
				&u.InputTokens, &u.OutputTokens, &reasoning, &u.CacheReadTokens, &u.CacheWriteTokens); err != nil {
				rows.Close()
				return nil, err
			}
			u.OutputTokens += reasoning
			u.CostKnown, u.UpdatedAt = true, s.LastActive
			s.Usage = &u
			out = append(out, s)
		}
		rows.Close()
		return out, rows.Err()
	}
	return nil, lastErr
}

func sortNewest(s []model.PastSession) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].LastActive > s[j].LastActive })
}

// Merge combines lists, newest first, capped at limit.
func Merge(limit int, lists ...[]model.PastSession) []model.PastSession {
	var out []model.PastSession
	for _, l := range lists {
		out = append(out, l...)
	}
	sortNewest(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
