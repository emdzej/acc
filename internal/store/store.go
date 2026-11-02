// Package store persists sessions, events, groups and messages in SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver keeps the binary CGO-free

	"github.com/emdzej/acc/internal/model"
)

const (
	maxPayload         = 64 << 10
	maxSubagentsListed = 20
	launchTimeout      = 10 * time.Minute
)

var (
	ErrNotFound = errors.New("not found")
	colorRe     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  agent TEXT NOT NULL,
  cwd TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  last_event TEXT NOT NULL DEFAULT '',
  last_tool TEXT NOT NULL DEFAULT '',
  last_message TEXT NOT NULL DEFAULT '',
  transcript_path TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL,
  name TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS events_session ON events(session_id, ts);
CREATE TABLE IF NOT EXISTS groups (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_groups (
  session_id TEXT NOT NULL,
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  PRIMARY KEY (session_id, group_id)
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL,
  body TEXT NOT NULL,
  sender TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  delivered_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_pending ON messages(session_id, delivered_at);
`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY churn.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrations are applied in order; PRAGMA user_version records how many ran.
// Append only: never edit a migration that has shipped.
var migrations = []string{
	schema,
	`ALTER TABLE events ADD COLUMN tool_name TEXT NOT NULL DEFAULT '';
	 ALTER TABLE events ADD COLUMN summary TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sessions ADD COLUMN color TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE events ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
	 CREATE TABLE subagents (
	   session_id TEXT NOT NULL,
	   id TEXT NOT NULL,
	   type TEXT NOT NULL DEFAULT '',
	   status TEXT NOT NULL,
	   last_tool TEXT NOT NULL DEFAULT '',
	   summary TEXT NOT NULL DEFAULT '',
	   result TEXT NOT NULL DEFAULT '',
	   transcript_path TEXT NOT NULL DEFAULT '',
	   started_at INTEGER NOT NULL,
	   last_seen INTEGER NOT NULL,
	   PRIMARY KEY (session_id, id)
	 );`,
	// Group tree: names are unique per parent (0 = root) and colour may be
	// empty (inherit). Rebuilt per https://sqlite.org/lang_altertable.html with
	// foreign keys off (see migrate) so session_groups rows survive the DROP.
	`CREATE TABLE groups_new (
	   id INTEGER PRIMARY KEY,
	   parent_id INTEGER NOT NULL DEFAULT 0,
	   name TEXT NOT NULL,
	   color TEXT NOT NULL DEFAULT '',
	   UNIQUE (parent_id, name)
	 );
	 INSERT INTO groups_new (id, parent_id, name, color) SELECT id, 0, name, color FROM groups;
	 DROP TABLE groups;
	 ALTER TABLE groups_new RENAME TO groups;`,
	`ALTER TABLE sessions ADD COLUMN attention TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sessions ADD COLUMN attention_detail TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sessions ADD COLUMN attention_since INTEGER NOT NULL DEFAULT 0;`,
	`ALTER TABLE sessions ADD COLUMN usage TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE sessions ADD COLUMN custom_title TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sessions ADD COLUMN ai_title TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sessions ADD COLUMN context_pct REAL;
	 ALTER TABLE sessions ADD COLUMN context_size INTEGER NOT NULL DEFAULT 0;
	 CREATE TABLE plan_usage (agent TEXT PRIMARY KEY, windows TEXT NOT NULL, updated_at INTEGER NOT NULL);`,
	`ALTER TABLE sessions ADD COLUMN acc_title TEXT NOT NULL DEFAULT '';`,
}

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= len(migrations) {
		return nil
	}
	// Table rebuilds must not trigger ON DELETE CASCADE. The pool has a single
	// connection, so these pragmas apply to the connection running the migrations.
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys = ON`)
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	var violations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		return err
	}
	if violations > 0 {
		return fmt.Errorf("migration left %d foreign key violations", violations)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// statusFor maps a Claude Code-style event name to a session status.
// Adapters for other agents normalize their event names to these.
func statusFor(event string) string {
	switch event {
	case "SessionStart":
		return model.StatusActive
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PreCompact":
		return model.StatusWorking
	case "Notification", "PermissionRequest":
		return model.StatusWaiting
	case "Stop":
		return model.StatusIdle
	case "SessionEnd":
		return model.StatusEnded
	}
	return ""
}

// RecordEvent upserts the session, appends the event and, when ev.Deliver is
// set, atomically claims the session's pending messages.
func (s *Store) RecordEvent(ctx context.Context, ev model.Event) ([]model.Message, error) {
	if ev.SessionID == "" {
		return nil, errors.New("session_id required")
	}
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	status := statusFor(ev.Name)
	toolName, lastMsg := ev.ToolName, truncate(ev.Message, 500)
	if ev.AgentID != "" {
		if err := upsertSubagent(ctx, tx, ev); err != nil {
			return nil, err
		}
		// A subagent's activity keeps its parent alive (and working, unless the
		// parent is waiting on the user) but doesn't overwrite the parent's own state.
		toolName, lastMsg = "", ""
		if status != "" && status != model.StatusWaiting {
			status = model.StatusWorking
		}
		if ev.Name == "SubagentStop" || ev.Name == "SubagentStart" {
			status = ""
		}
	}
	title := ""
	if ev.Cwd != "" {
		title = filepath.Base(ev.Cwd)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO sessions (id, agent, cwd, title, status, last_event, last_tool, last_message, transcript_path, started_at, last_seen)
VALUES (?, ?, ?, ?, COALESCE(NULLIF(?, ''), 'active'), ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  cwd = COALESCE(NULLIF(excluded.cwd, ''), cwd),
  title = CASE WHEN title = '' THEN excluded.title ELSE title END,
  status = COALESCE(NULLIF(?, ''), status),
  last_event = excluded.last_event,
  last_tool = CASE WHEN excluded.last_tool <> '' THEN excluded.last_tool ELSE last_tool END,
  last_message = CASE WHEN excluded.last_message <> '' THEN excluded.last_message ELSE last_message END,
  transcript_path = COALESCE(NULLIF(excluded.transcript_path, ''), transcript_path),
  last_seen = excluded.last_seen`,
		ev.SessionID, ev.Agent, ev.Cwd, title, status, ev.Name, toolName, lastMsg,
		ev.TranscriptPath, ev.TS, ev.TS, status)
	if err != nil {
		return nil, err
	}
	if att, detail, ok := attentionFor(ev); ok {
		if _, err = tx.ExecContext(ctx, `
UPDATE sessions SET attention = ?, attention_detail = ?,
  attention_since = CASE WHEN ? = '' THEN 0 WHEN attention = ? THEN attention_since ELSE ? END
WHERE id = ?`, att, truncate(detail, 500), att, att, ev.TS, ev.SessionID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events (session_id, agent_id, name, tool_name, summary, payload, ts) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.SessionID, ev.AgentID, ev.Name, ev.ToolName, truncate(ev.Summary, 300), truncate(string(ev.Raw), maxPayload), ev.TS); err != nil {
		return nil, err
	}

	var msgs []model.Message
	// Only the main agent may claim messages: a subagent's hook output would
	// inject them into the subagent's context, not the session the user addressed.
	if ev.Deliver && ev.AgentID == "" {
		if msgs, err = claimPending(ctx, tx, ev.SessionID, ev.TS); err != nil {
			return nil, err
		}
	}
	return msgs, tx.Commit()
}

// questionTools are tools whose call means the agent is asking the user something.
var questionTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

// attentionFor maps an event to the session's "waiting on you" state.
// ok=false leaves the current state unchanged.
func attentionFor(ev model.Event) (state, detail string, ok bool) {
	sub := ev.AgentID != ""
	switch ev.Name {
	case "PermissionRequest":
		return model.AttentionPermission, firstNonEmpty(ev.ToolName+": "+ev.Summary, ev.Message), true
	case "Notification":
		switch ev.NotificationType {
		case "permission_prompt":
			return model.AttentionPermission, ev.Message, true
		case "idle_prompt":
			return model.AttentionInput, ev.Message, true
		case "elicitation_dialog":
			return model.AttentionQuestion, ev.Message, true
		case "auth_success":
			return "", "", false
		}
		// Unknown or future notification kinds still mean "look at me".
		return model.AttentionNotice, ev.Message, true
	case "PreToolUse":
		if questionTools[ev.ToolName] {
			return model.AttentionQuestion, firstNonEmpty(ev.Summary, ev.ToolName), true
		}
		return "", "", true // a tool is running, so any approval was granted
	case "PostToolUse", "PostToolUseFailure", "UserPromptSubmit", "SessionEnd", "SubagentStart":
		return "", "", true
	case "Stop":
		if !sub {
			return model.AttentionInput, "turn finished", true
		}
	}
	return "", "", false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" && s != ": " {
			return s
		}
	}
	return ""
}

func upsertSubagent(ctx context.Context, tx *sql.Tx, ev model.Event) error {
	status := model.SubagentRunning
	result := ""
	if ev.Name == "SubagentStop" {
		status, result = model.SubagentDone, truncate(ev.Message, 2000)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO subagents (session_id, id, type, status, last_tool, summary, result, transcript_path, started_at, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, id) DO UPDATE SET
  type = COALESCE(NULLIF(excluded.type, ''), type),
  status = excluded.status,
  last_tool = COALESCE(NULLIF(excluded.last_tool, ''), last_tool),
  summary = COALESCE(NULLIF(excluded.summary, ''), summary),
  result = COALESCE(NULLIF(excluded.result, ''), result),
  transcript_path = COALESCE(NULLIF(excluded.transcript_path, ''), transcript_path),
  last_seen = excluded.last_seen`,
		ev.SessionID, ev.AgentID, ev.AgentType, status, ev.ToolName, truncate(ev.Summary, 300), result,
		ev.AgentTranscriptPath, ev.TS, ev.TS)
	return err
}

// ClaimPending atomically marks a session's queued messages delivered and returns them.
func (s *Store) ClaimPending(ctx context.Context, sessionID string) ([]model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	msgs, err := claimPending(ctx, tx, sessionID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	return msgs, tx.Commit()
}

func claimPending(ctx context.Context, tx *sql.Tx, sessionID string, now int64) ([]model.Message, error) {
	rows, err := tx.QueryContext(ctx, `
UPDATE messages SET delivered_at = ? WHERE session_id = ? AND delivered_at = 0
RETURNING id, session_id, body, sender, created_at, delivered_at`, now, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Body, &m.Sender, &m.CreatedAt, &m.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListSessions(ctx context.Context) ([]model.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.agent, s.cwd, s.title, s.acc_title, s.custom_title, s.ai_title, s.model, s.context_pct, s.context_size, s.color, s.usage, s.attention, s.attention_detail, s.attention_since, s.status, s.last_event, s.last_tool, s.last_message,
       s.transcript_path, s.started_at, s.last_seen,
       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.delivered_at = 0)
FROM sessions s ORDER BY s.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	idx := map[string]int{}
	for rows.Next() {
		var x model.Session
		var usage string
		var ctxPct sql.NullFloat64
		if err := rows.Scan(&x.ID, &x.Agent, &x.Cwd, &x.DirTitle, &x.ACCTitle, &x.CustomTitle, &x.AITitle, &x.Model, &ctxPct, &x.ContextSize, &x.Color, &usage, &x.Attention, &x.AttentionDetail, &x.AttentionSince, &x.Status, &x.LastEvent, &x.LastTool,
			&x.LastMessage, &x.TranscriptPath, &x.StartedAt, &x.LastSeen, &x.PendingCount); err != nil {
			return nil, err
		}
		if ctxPct.Valid {
			x.ContextPct = &ctxPct.Float64
		}
		if x.Status == model.StatusStarting && time.Since(time.UnixMilli(x.LastSeen)) > launchTimeout {
			x.Status = model.StatusEnded // launched, but the agent never reported in (window closed, dialog declined)
		}
		x.Title = firstNonEmpty(x.ACCTitle, x.CustomTitle, x.AITitle, x.DirTitle, x.ID)
		if usage != "" {
			x.Usage = &model.Usage{}
			if json.Unmarshal([]byte(usage), x.Usage) != nil {
				x.Usage = nil
			}
		}
		x.Groups = []model.Group{}
		x.Subagents = []model.Subagent{}
		idx[x.ID] = len(out)
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tree, err := s.loadTree(ctx)
	if err != nil {
		return nil, err
	}
	grows, err := s.db.QueryContext(ctx, `SELECT session_id, group_id FROM session_groups`)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var sid string
		var gid int64
		if err := grows.Scan(&sid, &gid); err != nil {
			return nil, err
		}
		if i, ok := idx[sid]; ok {
			if g, ok := tree.byID[gid]; ok {
				out[i].Groups = append(out[i].Groups, *g)
			}
		}
	}
	if err := grows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		sort.Slice(out[i].Groups, func(a, b int) bool { return out[i].Groups[a].Path < out[i].Groups[b].Path })
	}

	// Latest subagents per session, capped so a busy session can't bloat the list.
	arows, err := s.db.QueryContext(ctx, `
SELECT session_id, id, type, status, last_tool, summary, result, transcript_path, started_at, last_seen FROM (
  SELECT *, ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY started_at DESC) AS rn FROM subagents
) WHERE rn <= ? ORDER BY session_id, started_at DESC`, maxSubagentsListed)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var a model.Subagent
		if err := arows.Scan(&a.SessionID, &a.ID, &a.Type, &a.Status, &a.LastTool, &a.Summary, &a.Result,
			&a.TranscriptPath, &a.StartedAt, &a.LastSeen); err != nil {
			return nil, err
		}
		if i, ok := idx[a.SessionID]; ok {
			out[i].Subagents = append(out[i].Subagents, a)
		}
	}
	return out, arows.Err()
}

// RecentEvents returns a session's latest events, newest first.
func (s *Store) RecentEvents(ctx context.Context, sessionID string, limit int) ([]model.EventSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, agent_id, tool_name, summary, ts FROM events WHERE session_id = ? ORDER BY ts DESC, id DESC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EventSummary{}
	for rows.Next() {
		var e model.EventSummary
		if err := rows.Scan(&e.ID, &e.Name, &e.AgentID, &e.ToolName, &e.Summary, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestPayload returns the newest event among names for a session's main
// agent (agentID "") or one of its subagents.
func (s *Store) LatestPayload(ctx context.Context, sessionID, agentID string, names []string) (name string, ts int64, payload []byte, err error) {
	q := `SELECT name, ts, payload FROM events WHERE session_id = ? AND agent_id = ? AND name IN (?` +
		strings.Repeat(",?", len(names)-1) + `) ORDER BY ts DESC, id DESC LIMIT 1`
	args := []any{sessionID, agentID}
	for _, n := range names {
		args = append(args, n)
	}
	var p string
	err = s.db.QueryRowContext(ctx, q, args...).Scan(&name, &ts, &p)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return name, ts, []byte(p), err
}

// TranscriptPath returns the transcript for a session, or for one of its
// subagents. Running subagents haven't reported a path yet, so it is derived
// from Claude Code's layout: <session>.jsonl → <session>/subagents/agent-<id>.jsonl.
func (s *Store) TranscriptPath(ctx context.Context, sessionID, agentID string) (string, error) {
	var main string
	if err := s.db.QueryRowContext(ctx, `SELECT transcript_path FROM sessions WHERE id = ?`, sessionID).Scan(&main); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if agentID == "" {
		return main, nil
	}
	var p string
	err := s.db.QueryRowContext(ctx, `SELECT transcript_path FROM subagents WHERE session_id = ? AND id = ?`, sessionID, agentID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	if p == "" && main != "" {
		p = filepath.Join(strings.TrimSuffix(main, ".jsonl"), "subagents", "agent-"+agentID+".jsonl")
	}
	return p, nil
}

// Messages returns a session's latest messages, newest first.
func (s *Store) Messages(ctx context.Context, sessionID string, limit int) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, body, sender, created_at, delivered_at FROM messages
WHERE session_id = ? ORDER BY id DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Message{}
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Body, &m.Sender, &m.CreatedAt, &m.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TitleTarget is a live session whose transcript may carry a new title.
type TitleTarget struct {
	ID, TranscriptPath string
}

// TitleTargets lists non-ended sessions seen since `since` that have a transcript.
func (s *Store) TitleTargets(ctx context.Context, since int64) ([]TitleTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, transcript_path FROM sessions
WHERE transcript_path <> '' AND (status <> ? OR last_seen >= ?)`, model.StatusEnded, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TitleTarget
	for rows.Next() {
		var t TitleTarget
		if err := rows.Scan(&t.ID, &t.TranscriptPath); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTitles stores the agent-side titles; it reports whether anything changed.
// Empty values never clear a known title (the tail window may just miss it).
func (s *Store) SetTitles(ctx context.Context, id, custom, ai string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE sessions SET custom_title = COALESCE(NULLIF(?, ''), custom_title), ai_title = COALESCE(NULLIF(?, ''), ai_title)
WHERE id = ? AND (custom_title <> COALESCE(NULLIF(?, ''), custom_title) OR ai_title <> COALESCE(NULLIF(?, ''), ai_title))`,
		custom, ai, id, custom, ai)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RecordStatus stores a status-line report: per-session model and context,
// and the agent account's plan windows. It reports whether anything changed,
// since status lines refresh far more often than their values move.
func (s *Store) RecordStatus(ctx context.Context, r model.StatusReport, now int64) (bool, error) {
	changed := false
	if r.SessionID != "" {
		var pct any
		if r.ContextPct != nil {
			pct = *r.ContextPct
		}
		res, err := s.db.ExecContext(ctx, `
UPDATE sessions SET model = COALESCE(NULLIF(?, ''), model), context_pct = COALESCE(?, context_pct),
  context_size = CASE WHEN ? > 0 THEN ? ELSE context_size END
WHERE id = ? AND (model IS NOT COALESCE(NULLIF(?, ''), model) OR context_pct IS NOT COALESCE(?, context_pct)
  OR (? > 0 AND context_size <> ?))`,
			r.Model, pct, r.ContextSize, r.ContextSize, r.SessionID, r.Model, pct, r.ContextSize, r.ContextSize)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		changed = n > 0
	}
	if len(r.Plan) > 0 && r.Agent != "" {
		b, err := json.Marshal(r.Plan)
		if err != nil {
			return changed, err
		}
		res, err := s.db.ExecContext(ctx, `
INSERT INTO plan_usage (agent, windows, updated_at) VALUES (?, ?, ?)
ON CONFLICT(agent) DO UPDATE SET windows = excluded.windows, updated_at = excluded.updated_at
WHERE plan_usage.windows <> excluded.windows OR excluded.updated_at - plan_usage.updated_at > 60000`,
			r.Agent, string(b), now)
		if err != nil {
			return changed, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed = true
		}
	}
	return changed, nil
}

func (s *Store) PlanUsage(ctx context.Context) ([]model.PlanUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT agent, windows, updated_at FROM plan_usage ORDER BY agent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PlanUsage{}
	for rows.Next() {
		var p model.PlanUsage
		var w string
		if err := rows.Scan(&p.Agent, &w, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(w), &p.Windows) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// SetUsage stores a session's latest usage snapshot.
func (s *Store) SetUsage(ctx context.Context, sessionID string, u *model.Usage) error {
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET usage = ? WHERE id = ?`, string(b), sessionID)
	return err
}

// PrepareSession records a session ACC is about to launch, so its group,
// title and colour apply from the first hook. The agent's SessionStart
// then fills in the rest.
func (s *Store) PrepareSession(ctx context.Context, id, agent, cwd string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (id, agent, cwd, title, status, last_event, started_at, last_seen)
VALUES (?, ?, ?, ?, ?, 'Launch', ?, ?)`, id, agent, cwd, filepath.Base(cwd), model.StatusStarting, now, now)
	return err
}

// DeleteSession removes a session and its group links (used to undo a failed launch).
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session_groups WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// SetSessionTitle sets the session's title in ACC, shown over the agent's
// own titles; "" clears it. It doesn't rename the session in the agent.
func (s *Store) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if len(title) > 200 {
		return errors.New("title too long (max 200 bytes)")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET acc_title = ? WHERE id = ?`, title, sessionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return nil
}

// SetSessionColor sets a session's own colour; "" clears it so the group colour shows.
func (s *Store) SetSessionColor(ctx context.Context, sessionID, color string) error {
	if color != "" && !colorRe.MatchString(color) {
		return fmt.Errorf("invalid colour %q, want #rrggbb", color)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET color = ? WHERE id = ?`, strings.ToLower(color), sessionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return nil
}

// ResolveSession accepts a full session ID or a unique prefix.
func (s *Store) ResolveSession(ctx context.Context, ref string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions WHERE id = ? OR id LIKE ? || '%' LIMIT 2`, ref, ref)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if id == ref {
			return id, nil
		}
		ids = append(ids, id)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("session %q: %w", ref, ErrNotFound)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("session prefix %q is ambiguous", ref)
}

// Enqueue queues a message for one session, or for every non-ended session in a group.
func (s *Store) Enqueue(ctx context.Context, req model.SendRequest, sender string) ([]model.Message, error) {
	if strings.TrimSpace(req.Body) == "" {
		return nil, errors.New("message body required")
	}
	var targets []string
	switch {
	case req.SessionID != "":
		id, err := s.ResolveSession(ctx, req.SessionID)
		if err != nil {
			return nil, err
		}
		targets = []string{id}
	case req.Group != "":
		tree, err := s.loadTree(ctx)
		if err != nil {
			return nil, err
		}
		g, err := tree.find(req.Group)
		if err != nil {
			return nil, err
		}
		ids := tree.subtree(g.ID)
		rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT s.id FROM sessions s
JOIN session_groups sg ON sg.session_id = s.id
WHERE sg.group_id IN (`+placeholders(len(ids))+`) AND s.status <> ?`, append(anySlice(ids), model.StatusEnded)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			targets = append(targets, id)
		}
		rows.Close()
		if len(targets) == 0 {
			return nil, fmt.Errorf("group %q has no live sessions", req.Group)
		}
	default:
		return nil, errors.New("session_id or group required")
	}

	now := time.Now().UnixMilli()
	out := make([]model.Message, 0, len(targets))
	for _, id := range targets {
		m := model.Message{SessionID: id, Body: req.Body, Sender: sender, CreatedAt: now}
		if err := s.db.QueryRowContext(ctx,
			`INSERT INTO messages (session_id, body, sender, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
			id, m.Body, m.Sender, m.CreatedAt).Scan(&m.ID); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") // don't leave a split rune at the cut
}
