package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/emdzej/acc/internal/agent"
	"github.com/emdzej/acc/internal/launch"
	"github.com/emdzej/acc/internal/model"
)

// launchSession opens a terminal running a new agent session. The command
// comes from ACC's own config, never from the request: callers choose only
// the directory, title, group and first prompt.
func (s *Server) launchSession(w http.ResponseWriter, r *http.Request) {
	var req model.LaunchRequest
	if !decode(w, r, &req) {
		return
	}
	cfg, err := launch.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	dir, err := launch.ResolveDir(req.Cwd)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Tmux != nil {
		cfg.Tmux = *req.Tmux
	}
	ctx := r.Context()
	a, ok := agent.Lookup(cmp.Or(req.Agent, agent.Claude))
	if !ok || !a.CanStart() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("ACC can't start %s sessions", cmp.Or(a.Name, req.Agent)))
		return
	}
	spec := launch.Spec{SessionID: launch.NewSessionID(), Cwd: dir, Name: strings.TrimSpace(req.Title), Prompt: strings.TrimSpace(req.Prompt), Agent: a.ID}
	if err := s.store.PrepareSession(ctx, spec.SessionID, a.ID, dir); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Apply the extras before the window opens; a bad group or colour fails
	// the launch rather than starting a session the user didn't ask for.
	fail := func(code int, err error) {
		s.store.DeleteSession(ctx, spec.SessionID)
		writeErr(w, code, err)
	}
	if spec.Name != "" {
		if err := s.store.SetSessionTitle(ctx, spec.SessionID, spec.Name); err != nil {
			fail(http.StatusBadRequest, err)
			return
		}
	}
	if req.Group != "" {
		if err := s.store.Assign(ctx, spec.SessionID, req.Group, false); err != nil {
			fail(statusOf(err), err)
			return
		}
	}
	if req.Color != "" {
		if err := s.store.SetSessionColor(ctx, spec.SessionID, req.Color); err != nil {
			fail(http.StatusBadRequest, err)
			return
		}
	}
	host, err := cfg.Start(spec)
	if err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	var hp *model.Host
	if host.Kind != "" {
		hp = &host
		if err := s.store.SetHost(ctx, spec.SessionID, host); err != nil {
			s.log.Warn("record host", "session", spec.SessionID, "err", err)
		}
	}
	s.repaint(ctx)
	line, _ := cfg.CommandLine(spec)
	s.log.Info("session launched", "audit", true, "session", spec.SessionID, "agent", a.ID, "cwd", dir, "terminal", cfg.Terminal, "tmux", cfg.Tmux, "prompt_bytes", len(spec.Prompt))
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, model.LaunchResponse{SessionID: spec.SessionID, Terminal: cfg.Terminal, Command: line, Host: hp})
}

// typeInto types text plus Enter into a session ACC started. Refuses while
// the session shows a permission or question dialog (unless forced), where
// the keystrokes would answer the dialog instead.
func (s *Server) typeInto(ctx context.Context, id, text string, force bool) error {
	h, _, attention, err := s.store.SessionHost(ctx, id)
	if err != nil {
		return err
	}
	if h.Kind == "" {
		return errNoHost
	}
	if !force && (attention == model.AttentionPermission || attention == model.AttentionQuestion) {
		return fmt.Errorf("session is showing a %s dialog: answer it first (or force)", attention)
	}
	if err := launch.Type(h, text); err != nil {
		return err
	}
	s.log.Info("typed into session", "audit", true, "session", id, "host", h.Kind, "bytes", len(text))
	return nil
}

// repaint gives tmux-hosted sessions their current colour and title. Run
// after anything that can change them: colours, titles, group membership.
func (s *Server) repaint(ctx context.Context) {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return
	}
	for _, x := range sessions {
		if x.Host == nil || x.Host.Kind != launch.HostTmux || x.Status == model.StatusEnded {
			continue
		}
		if err := launch.Paint(*x.Host, x.DisplayColor(), x.Title); err != nil {
			s.log.Debug("paint tmux session", "session", x.ID, "err", err)
		}
	}
}

// openDir opens the session's directory in a terminal or the file manager,
// on the machine running ACC.
func (s *Server) openDir(w http.ResponseWriter, r *http.Request) {
	var req model.OpenRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	id, err := s.store.ResolveSession(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	_, cwd, _, err := s.store.SessionHost(ctx, id)
	if err == nil {
		cwd, err = launch.ResolveDir(cwd)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("session directory: %w", err))
		return
	}
	switch req.App {
	case model.OpenTerminal:
		var cfg launch.Config
		if cfg, err = launch.Load(); err == nil {
			err = cfg.Shell(cwd)
		}
	case model.OpenFiles:
		err = launch.Reveal(cwd)
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("app must be %s or %s", model.OpenTerminal, model.OpenFiles))
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.log.Info("opened session directory", "audit", true, "session", id, "app", req.App, "cwd", cwd)
	w.WriteHeader(http.StatusNoContent)
}

var errNoHost = errors.New("slash commands need a session ACC started (acc new / N / + Session) in tmux or Ghostty")

func (s *Server) typeHandler(w http.ResponseWriter, r *http.Request) {
	var req model.TypeRequest
	if !decode(w, r, &req) {
		return
	}
	id, err := s.store.ResolveSession(r.Context(), r.PathValue("id"))
	if err == nil {
		if strings.TrimSpace(req.Text) == "" {
			err = errors.New("text required")
		} else {
			err = s.typeInto(r.Context(), id, req.Text, req.Force)
		}
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// attach brings a session ACC started back on screen.
func (s *Server) attach(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := s.store.ResolveSession(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	h, cwd, _, err := s.store.SessionHost(ctx, id)
	if err == nil && h.Kind == "" {
		err = errors.New("ACC didn't start this session, so it can't bring it back")
	}
	var cfg launch.Config
	if err == nil {
		cfg, err = launch.Load()
	}
	if err == nil {
		err = cfg.Attach(h, cwd)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
