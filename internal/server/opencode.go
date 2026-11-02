package server

import (
	"net/http"

	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/opencode"
)

// postOpenCode receives raw OpenCode bus events from the ACC plugin and
// applies what they mean: lifecycle events, titles, usage, model, goal.
func (s *Server) postOpenCode(w http.ResponseWriter, r *http.Request) {
	var raw opencode.RawEvent
	if !decode(w, r, &raw) {
		return
	}
	res := opencode.Translate(raw)
	ctx := r.Context()
	changed := false
	if res.Event != nil {
		if _, err := s.store.RecordEvent(ctx, *res.Event); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		changed = true
	}
	sid := ""
	if res.Event != nil {
		sid = res.Event.SessionID
	}
	var d struct {
		SessionID string `json:"sessionID"`
	}
	if sid == "" {
		_ = decodeRaw(raw.Data, &d)
		sid = d.SessionID
	}
	if sid != "" {
		if res.Title != "" {
			if ok, err := s.store.SetTitles(ctx, sid, "", res.Title); err == nil && ok {
				changed = true
			}
		}
		if res.Usage != nil {
			if err := s.store.SetUsage(ctx, sid, res.Usage); err == nil {
				changed = true
			}
		}
		if res.GoalSet {
			if ok, err := s.store.SetGoal(ctx, sid, res.Goal); err == nil && ok {
				changed = true
			}
		}
		if res.Model != "" {
			if ok, err := s.store.RecordStatus(ctx, model.StatusReport{Agent: opencode.Agent, SessionID: sid, Model: res.Model}, 0); err == nil && ok {
				changed = true
			}
		}
	}
	if changed {
		s.hub.publish("sessions")
	}
	w.WriteHeader(http.StatusNoContent)
}
