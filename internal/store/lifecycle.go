package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/emdzej/acc/internal/model"
)

// ForgetSession removes a session and everything ACC recorded about it:
// events, messages, subagents and group links. The agent's own records are
// untouched, and the session comes back if it reports in again.
func (s *Store) ForgetSession(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := forget(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ForgetEnded forgets every ended session, including launches that never
// reported in, and returns how many.
func (s *Store) ForgetEnded(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE status = ? OR (status = ? AND last_seen < ?)`,
		model.StatusEnded, model.StatusStarting, time.Now().Add(-launchTimeout).UnixMilli())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := forget(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), tx.Commit()
}

func forget(ctx context.Context, tx *sql.Tx, id string) error {
	for _, q := range []string{
		`DELETE FROM session_aliases WHERE session_id = ?`,
		`DELETE FROM session_groups WHERE session_id = ?`,
		`DELETE FROM messages WHERE session_id = ?`,
		`DELETE FROM events WHERE session_id = ?`,
		`DELETE FROM subagents WHERE session_id = ?`,
		`DELETE FROM sessions WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// TrackSession adds a session from an agent's records (see package
// history) as ended, or idle if its process is running, so it can be
// opened, grouped and resumed. A session ACC already lists is left as it is.
func (s *Store) TrackSession(ctx context.Context, p model.PastSession) error {
	var usage string
	if p.Usage != nil {
		b, err := json.Marshal(p.Usage)
		if err != nil {
			return err
		}
		usage = string(b)
	}
	started := cmp.Or(p.StartedAt, p.LastActive)
	status := model.StatusEnded
	if p.Running {
		status = model.StatusIdle
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (id, agent, cwd, title, ai_title, status, last_event, transcript_path, usage, started_at, last_seen)
VALUES (?, ?, ?, ?, ?, ?, 'History', ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING`, p.ID, p.Agent, p.Cwd, filepath.Base(p.Cwd), p.Title, status,
		p.TranscriptPath, usage, started, p.LastActive)
	return err
}

// MarkResuming shows a session as starting again after ACC reopened it in a
// new window; its hooks then take over. Its old window is forgotten. With
// atPrompt (agents that report in only at the first prompt), it shows as
// idle and awaiting input instead, which is where the agent is.
func (s *Store) MarkResuming(ctx context.Context, id string, atPrompt bool) error {
	status, attention, detail, since := model.StatusStarting, "", "", int64(0)
	now := time.Now().UnixMilli()
	if atPrompt {
		status, attention, detail, since = model.StatusIdle, model.AttentionInput, "resumed", now
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE sessions SET status = ?, last_event = 'Resume', last_seen = ?, attention = ?, attention_detail = ?, attention_since = ?,
  host = '', host_ref = '' WHERE id = ?`, status, now, attention, detail, since, id)
	return err
}

// MarkGone ends a session whose process is known to have exited without
// saying so (a closed terminal, a crash): no SessionEnd will come.
func (s *Store) MarkGone(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sessions SET status = ?, last_event = 'Gone', attention = '', attention_detail = '', attention_since = 0
WHERE id = ? AND status <> ?`, model.StatusEnded, id, model.StatusEnded)
	return err
}

// adoptLaunch gives the session ACC registered when it started one (under
// ev.LaunchID, for an agent that picks its own IDs) the agent's real ID, so
// its title, groups, colour, host and queued messages carry over. It does
// nothing unless that registration is still waiting for its session, of
// the same agent, and the real ID is new to ACC.
func adoptLaunch(ctx context.Context, tx *sql.Tx, ev model.Event) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM sessions WHERE id = ? AND agent = ? AND status = ?) -
  (SELECT COUNT(*) FROM sessions WHERE id = ?)`, ev.LaunchID, ev.Agent, model.StatusStarting, ev.SessionID).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return nil
	}
	for _, q := range []string{
		`UPDATE sessions SET id = ? WHERE id = ?`,
		`UPDATE session_groups SET session_id = ? WHERE session_id = ?`,
		`UPDATE messages SET session_id = ? WHERE session_id = ?`,
		`UPDATE events SET session_id = ? WHERE session_id = ?`,
		`UPDATE subagents SET session_id = ? WHERE session_id = ?`,
		`INSERT OR REPLACE INTO session_aliases (session_id, alias) VALUES (?, ?)`,
	} {
		if _, err := tx.ExecContext(ctx, q, ev.SessionID, ev.LaunchID); err != nil {
			return err
		}
	}
	return nil
}
