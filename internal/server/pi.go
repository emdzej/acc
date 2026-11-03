package server

import (
	"net/http"

	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/pi"
)

// postPi receives raw pi events from ACC's extension and applies what they
// mean: lifecycle events, the session name, model and context use. Usage
// comes from the session file (see usageReaders).
func (s *Server) postPi(w http.ResponseWriter, r *http.Request) {
	var raw pi.RawEvent
	if !decode(w, r, &raw) {
		return
	}
	res := pi.Translate(raw)
	ctx := r.Context()
	sid := raw.Session.ID
	if sid == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	changed := false
	if res.Event != nil {
		if _, err := s.store.RecordEvent(ctx, *res.Event); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		s.usage.observe(*res.Event)
		changed = true
	}
	if res.Title != "" {
		if ok, err := s.store.SetTitles(ctx, sid, res.Title, ""); err == nil && ok {
			changed = true
		}
	}
	if res.Model != "" || res.ContextPct != nil {
		st := model.StatusReport{Agent: pi.Agent, SessionID: sid, Model: res.Model, ContextPct: res.ContextPct, ContextSize: res.ContextSize}
		if ok, err := s.store.RecordStatus(ctx, st, 0); err == nil && ok {
			changed = true
		}
	}
	if changed {
		s.hub.publish("sessions")
	}
	w.WriteHeader(http.StatusNoContent)
}
