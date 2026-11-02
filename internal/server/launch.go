package server

import (
	"net/http"
	"strings"

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
	ctx := r.Context()
	spec := launch.Spec{SessionID: launch.NewSessionID(), Cwd: dir, Name: strings.TrimSpace(req.Title), Prompt: strings.TrimSpace(req.Prompt)}
	if err := s.store.PrepareSession(ctx, spec.SessionID, "claude-code", dir); err != nil {
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
	if err := cfg.Start(spec); err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	s.log.Info("session launched", "audit", true, "session", spec.SessionID, "cwd", dir, "terminal", cfg.Terminal, "prompt_bytes", len(spec.Prompt))
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, model.LaunchResponse{SessionID: spec.SessionID, Terminal: cfg.Terminal, Command: cfg.CommandLine(spec)})
}
