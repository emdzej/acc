// Package codex supports OpenAI's Codex CLI. Codex reports through hooks
// shaped like Claude Code's (see internal/hook); this package reads what
// hooks don't carry from Codex's own files: usage, plan limits and context
// from a session's rollout, and past sessions from its state database. It
// only reads; nothing here changes Codex's files.
package codex

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/emdzej/acc/internal/model"
)

const Agent = "codex"

// Home is Codex's home directory: $CODEX_HOME, else ~/.codex.
func Home() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// ErrForbidden: the path isn't a Codex rollout.
var ErrForbidden = errors.New("not a Codex session rollout")

// checkPath allows only .jsonl files under Codex's sessions directories,
// since the path comes from a hook event.
func checkPath(path string) (string, error) {
	if !strings.HasSuffix(path, ".jsonl") {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, d := range []string{"sessions", "archived_sessions"} {
		root, err := filepath.EvalSymlinks(filepath.Join(Home(), d))
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, real); err == nil && !strings.HasPrefix(rel, "..") {
			return real, nil
		}
	}
	return "", ErrForbidden
}

// ---- rollouts -----------------------------------------------------------------

// line is one rollout record: {"timestamp", "type", "payload"}.
type line struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type tokenUsage struct {
	Input      int64 `json:"input_tokens"` // includes cached input
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"` // includes reasoning
	Total      int64 `json:"total_tokens"`
}

type window struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes *int64  `json:"window_minutes"`
	ResetsAt      *int64  `json:"resets_at"` // unix seconds
}

// tokenCount is an event_msg of type token_count.
type tokenCount struct {
	Type string `json:"type"`
	Info *struct {
		Total         tokenUsage `json:"total_token_usage"`
		Last          tokenUsage `json:"last_token_usage"`
		ContextWindow *int64     `json:"model_context_window"`
	} `json:"info"`
	RateLimits *struct {
		Primary   *window `json:"primary"`
		Secondary *window `json:"secondary"`
	} `json:"rate_limits"`
}

// Rollout reads a session's rollout: usage totals, plus a status report
// with the model, context use and the account's plan limits when the
// rollout carries them. Codex records no cost.
func Rollout(path string) (*model.Usage, *model.StatusReport, error) {
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
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			readLine(b, u, st, models)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return u, st, nil
}

func readLine(b []byte, u *model.Usage, st *model.StatusReport, models map[string]bool) {
	var l line
	if json.Unmarshal(b, &l) != nil {
		return
	}
	switch l.Type {
	case "turn_context":
		var tc struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(l.Payload, &tc) == nil && tc.Model != "" {
			st.Model = tc.Model
			if !models[tc.Model] {
				models[tc.Model] = true
				u.Models = append(u.Models, tc.Model)
			}
		}
	case "event_msg":
		var tc tokenCount
		if json.Unmarshal(l.Payload, &tc) != nil || tc.Type != "token_count" {
			return
		}
		if i := tc.Info; i != nil {
			// Totals are running sums: the latest wins.
			t := i.Total
			u.InputTokens, u.CacheReadTokens = t.Input-t.Cached, t.Cached
			u.CacheWriteTokens, u.OutputTokens = t.CacheWrite, t.Output
			if i.ContextWindow != nil && *i.ContextWindow > 0 {
				pct := float64(i.Last.Total) / float64(*i.ContextWindow) * 100
				st.ContextPct, st.ContextSize = &pct, *i.ContextWindow
			}
		}
		if rl := tc.RateLimits; rl != nil {
			var plan []model.PlanWindow
			for _, w := range []*window{rl.Primary, rl.Secondary} {
				if w != nil {
					plan = append(plan, planWindow(*w))
				}
			}
			if len(plan) > 0 {
				st.Plan = plan
			}
		}
	}
}

// planWindow names a rate-limit window by its length, as ACC shows Claude
// Code's: five_hour (5h) and seven_day (7d).
func planWindow(w window) model.PlanWindow {
	pw := model.PlanWindow{UsedPct: w.UsedPercent}
	if w.ResetsAt != nil {
		pw.ResetsAt = *w.ResetsAt * 1000
	}
	mins := int64(0)
	if w.WindowMinutes != nil {
		mins = *w.WindowMinutes
	}
	switch {
	case mins == 300:
		pw.Key, pw.Label = "five_hour", "5h"
	case mins == 7*24*60:
		pw.Key, pw.Label = "seven_day", "7d"
	case mins > 0 && mins%(24*60) == 0:
		pw.Key, pw.Label = fmt.Sprintf("%d_day", mins/(24*60)), fmt.Sprintf("%dd", mins/(24*60))
	case mins > 0 && mins%60 == 0:
		pw.Key, pw.Label = fmt.Sprintf("%d_hour", mins/60), fmt.Sprintf("%dh", mins/60)
	case mins > 0:
		pw.Key, pw.Label = fmt.Sprintf("%d_min", mins), fmt.Sprintf("%dm", mins)
	default:
		pw.Key, pw.Label = "window", "limit"
	}
	return pw
}

// ---- history ------------------------------------------------------------------

// StateDB is Codex's state database, the newest schema version present
// (state_5.sqlite, …), or "" if there is none.
func StateDB() string {
	files, _ := filepath.Glob(filepath.Join(Home(), "state_*.sqlite"))
	version := func(f string) int {
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "state_"), ".sqlite"))
		return n
	}
	sort.Slice(files, func(i, j int) bool { return version(files[i]) > version(files[j]) })
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

// Sessions lists Codex's sessions, newest first: the threads in its state
// database that have a prompt or a name, without archived ones and
// subagents. (has_user_event isn't a guide: Codex 0.166 leaves it 0.)
// It reads the database read-only; a missing or unfamiliar one yields nothing.
func Sessions(ctx context.Context, dbPath string, limit int) ([]model.PastSession, error) {
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
SELECT id, rollout_path, cwd, COALESCE(NULLIF(name, ''), NULLIF(title, ''), first_user_message),
       COALESCE(created_at_ms, created_at * 1000), COALESCE(updated_at_ms, updated_at * 1000)
FROM threads
WHERE archived = 0 AND (first_user_message <> '' OR COALESCE(name, '') <> '')
  AND COALESCE(agent_path, '') = '' AND COALESCE(agent_role, '') = ''
ORDER BY COALESCE(updated_at_ms, updated_at * 1000) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PastSession
	for rows.Next() {
		s := model.PastSession{Agent: Agent}
		if err := rows.Scan(&s.ID, &s.TranscriptPath, &s.Cwd, &s.Title, &s.StartedAt, &s.LastActive); err != nil {
			return nil, err
		}
		s.Title = clip(strings.Join(strings.Fields(s.Title), " "), 80)
		if fi, err := os.Stat(s.TranscriptPath); err == nil {
			s.Size = fi.Size()
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
