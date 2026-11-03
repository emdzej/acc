package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/agent"
	"github.com/emdzej/acc/internal/history"
	"github.com/emdzej/acc/internal/launch"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/store"
)

const (
	goneEvery = 20 * time.Second
	// goneGrace keeps a just-active session alive while Claude Code is still
	// writing its registry entry.
	goneGrace = time.Minute
)

// pastSessions lists the sessions agents have on record, marking those ACC lists.
func (s *Server) pastSessions(ctx context.Context, limit int) ([]model.PastSession, error) {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, x := range sessions {
		known[x.ID] = true
	}
	out, err := s.hist.All(ctx, limit)
	if err != nil {
		s.log.Debug("read agents' sessions", "err", err)
	}
	for i := range out {
		out[i].Tracked = known[out[i].ID]
	}
	return out, nil
}

func (s *Server) listHistory(w http.ResponseWriter, r *http.Request) {
	out, err := s.pastSessions(r.Context(), limitParam(r, 300, 5000))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []model.PastSession{}
	}
	writeJSON(w, http.StatusOK, out)
}

// findPast resolves a session id, or a unique prefix, in the agents' records.
func (s *Server) findPast(ctx context.Context, ref string) (model.PastSession, error) {
	past, err := s.pastSessions(ctx, 1<<20)
	if err != nil {
		return model.PastSession{}, err
	}
	var hits []model.PastSession
	for _, p := range past {
		if p.ID == ref {
			return p, nil
		}
		if strings.HasPrefix(p.ID, ref) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return model.PastSession{}, fmt.Errorf("session %q: %w", ref, store.ErrNotFound)
	case 1:
		return hits[0], nil
	}
	return model.PastSession{}, fmt.Errorf("session prefix %q is ambiguous", ref)
}

// session finds a session ACC lists or, failing that, one in the agents'
// records, which ACC then starts listing.
func (s *Server) session(ctx context.Context, ref string) (model.Session, error) {
	id, err := s.store.ResolveSession(ctx, ref)
	if errors.Is(err, store.ErrNotFound) {
		var p model.PastSession
		if p, err = s.findPast(ctx, ref); err == nil {
			id, err = p.ID, s.track(ctx, p)
		}
	}
	if err != nil {
		return model.Session{}, err
	}
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return model.Session{}, err
	}
	for _, x := range sessions {
		if x.ID == id {
			return x, nil
		}
	}
	return model.Session{}, fmt.Errorf("session %q: %w", id, store.ErrNotFound)
}

func (s *Server) track(ctx context.Context, p model.PastSession) error {
	if err := s.store.TrackSession(ctx, p); err != nil {
		return err
	}
	if p.TranscriptPath != "" {
		s.usage.observe(model.Event{Agent: p.Agent, SessionID: p.ID, Name: "SessionEnd", TranscriptPath: p.TranscriptPath})
	}
	s.log.Info("session added from history", "session", p.ID, "agent", p.Agent)
	s.hub.publish("sessions")
	return nil
}

// trackSession adds a session from the agents' records to ACC, as ended.
func (s *Server) trackSession(w http.ResponseWriter, r *http.Request) {
	x, err := s.session(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

// resume puts a session back on screen: its window if ACC opened one that
// is still there, else a new window continuing the session in its directory.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req model.ResumeRequest
	if r.ContentLength > 0 && !decode(w, r, &req) {
		return
	}
	x, err := s.session(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	cfg, err := launch.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// A window ACC opened that is gone took the agent with it.
	hostGone := false
	if x.Host != nil {
		if launch.Alive(*x.Host) {
			if err := cfg.Attach(*x.Host, x.Cwd); err != nil {
				writeErr(w, http.StatusBadGateway, err)
				return
			}
			writeJSON(w, http.StatusOK, model.ResumeResponse{SessionID: x.ID, Action: model.ResumeAttached})
			return
		}
		hostGone = true
	}
	a := agent.Get(x.Agent)
	if !a.CanResume() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("ACC can't resume %s sessions", a.Name))
		return
	}
	// Two processes on one session would both write its transcript.
	if pid, ok := s.runningPID(x); ok {
		writeErr(w, http.StatusConflict, fmt.Errorf("the session is still running (pid %d) in a terminal ACC didn't open: switch to that window", pid))
		return
	}
	if x.Status != model.StatusEnded && !hostGone && !s.knowsRunning(x) {
		// ACC can't see the process; trust the session's own state.
		writeErr(w, http.StatusConflict, fmt.Errorf("the session looks live (%s): switch to its window, or forget it first if it has exited", x.Status))
		return
	}
	dir, err := launch.ResolveDir(x.Cwd)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("session directory: %w", err))
		return
	}
	if req.Tmux != nil {
		cfg.Tmux = *req.Tmux
	}
	spec := launch.Spec{SessionID: x.ID, Cwd: dir, Resume: true, Agent: x.Agent}
	host, err := cfg.Start(spec)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if err := s.store.MarkResuming(ctx, x.ID, a.LazyStart); err != nil {
		s.log.Warn("mark resuming", "session", x.ID, "err", err)
	}
	if host.Kind != "" {
		if err := s.store.SetHost(ctx, x.ID, host); err != nil {
			s.log.Warn("record host", "session", x.ID, "err", err)
		}
	}
	s.repaint(ctx)
	s.log.Info("session resumed", "audit", true, "session", x.ID, "agent", x.Agent, "cwd", dir, "terminal", cfg.Terminal, "tmux", cfg.Tmux)
	s.hub.publish("sessions")
	line, _ := cfg.CommandLine(spec)
	writeJSON(w, http.StatusOK, model.ResumeResponse{SessionID: x.ID, Action: model.ResumeResumed, Terminal: cfg.Terminal, Command: line})
}

// knowsRunning reports whether ACC can tell if the session's process is
// alive, so a session not found running has really exited. Only Claude
// Code keeps a registry of its running sessions. OpenCode sessions are
// resumed even while another OpenCode has them open, as before.
func (s *Server) knowsRunning(x model.Session) bool {
	switch x.Agent {
	case agent.Claude:
		dir := history.ConfigDirOf(x.TranscriptPath)
		return dir != "" && history.HasRegistry(dir)
	case agent.OpenCode:
		return true
	}
	return false
}

// runningPID reports a Claude Code session's process, if it is alive.
func (s *Server) runningPID(x model.Session) (int, bool) {
	if x.Agent != agent.Claude {
		return 0, false
	}
	dirs := history.ClaudeConfigDirs()
	if d := history.ConfigDirOf(x.TranscriptPath); d != "" {
		dirs = append([]string{d}, dirs...)
	}
	for _, d := range dirs {
		if pid, ok := history.Running(d)[x.ID]; ok {
			return pid, true
		}
	}
	return 0, false
}

// forgetSession removes a session from ACC (not from the agent).
func (s *Server) forgetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := s.store.ResolveSession(ctx, r.PathValue("id"))
	if err == nil {
		err = s.store.ForgetSession(ctx, id)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	s.log.Info("session forgotten", "audit", true, "session", id)
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, model.ForgetResponse{Forgotten: 1})
}

// forgetEnded removes every ended session from ACC: DELETE /api/v1/sessions?status=ended.
func (s *Server) forgetEnded(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("status") != model.StatusEnded {
		writeErr(w, http.StatusBadRequest, errors.New("only ended sessions can be forgotten in bulk (?status=ended)"))
		return
	}
	n, err := s.store.ForgetEnded(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("ended sessions forgotten", "audit", true, "count", n)
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, model.ForgetResponse{Forgotten: n})
}

// sweepGone ends sessions whose process has exited without a SessionEnd (a
// closed terminal, a crash): Claude Code sessions missing from Claude
// Code's registry of running sessions, and sessions of any agent whose tmux
// session ACC started is gone. A session that reports in again comes back
// to life.
func (s *Server) sweepGone(ctx context.Context) {
	t := time.NewTicker(goneEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sessions, err := s.store.ListSessions(ctx)
		if err != nil {
			continue
		}
		running := map[string]map[string]int{} // config dir -> running sessions
		changed := false
		for _, x := range sessions {
			if x.Status == model.StatusEnded || time.Since(time.UnixMilli(x.LastSeen)) < goneGrace {
				continue
			}
			if x.Host != nil && x.Host.Kind == launch.HostTmux {
				if !launch.Alive(*x.Host) && s.store.MarkGone(ctx, x.ID) == nil {
					s.log.Info("session's tmux session gone", "session", x.ID)
					changed = true
				}
				continue
			}
			if x.Agent != agent.Claude || x.Status == model.StatusStarting {
				continue
			}
			dir := history.ConfigDirOf(x.TranscriptPath)
			if dir == "" || !history.HasRegistry(dir) {
				continue
			}
			if _, ok := running[dir]; !ok {
				running[dir] = history.Running(dir)
			}
			if _, ok := running[dir][x.ID]; ok {
				continue
			}
			if err := s.store.MarkGone(ctx, x.ID); err == nil {
				s.log.Info("session process gone", "session", x.ID)
				changed = true
			}
		}
		if changed {
			s.hub.publish("sessions")
		}
	}
}
