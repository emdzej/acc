package server

import (
	"context"
	"sync"
	"time"

	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

// usageThrottle bounds how often a busy session's transcript is re-read
// between turn boundaries.
const usageThrottle = 15 * time.Second

// usageForce lists events after which usage is recomputed immediately.
var usageForce = map[string]bool{"Stop": true, "SubagentStop": true, "SessionEnd": true, "UserPromptSubmit": true}

type usageRefresher struct {
	srv      *Server
	mu       sync.Mutex
	last     map[string]time.Time
	inflight map[string]bool
}

func newUsageRefresher(s *Server) *usageRefresher {
	return &usageRefresher{srv: s, last: map[string]time.Time{}, inflight: map[string]bool{}}
}

// observe schedules a background recompute for the event's session if due.
func (u *usageRefresher) observe(ev model.Event) {
	if ev.Agent != "claude-code" || ev.TranscriptPath == "" {
		return
	}
	u.mu.Lock()
	due := usageForce[ev.Name] || time.Since(u.last[ev.SessionID]) >= usageThrottle
	if !due || u.inflight[ev.SessionID] {
		u.mu.Unlock()
		return
	}
	u.inflight[ev.SessionID] = true
	u.last[ev.SessionID] = time.Now()
	u.mu.Unlock()

	go func() {
		defer func() {
			u.mu.Lock()
			delete(u.inflight, ev.SessionID)
			u.mu.Unlock()
		}()
		us, err := transcript.Usage(ev.TranscriptPath)
		if err != nil {
			u.srv.log.Debug("usage refresh failed", "session", ev.SessionID, "err", err)
			return
		}
		us.UpdatedAt = time.Now().UnixMilli()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := u.srv.store.SetUsage(ctx, ev.SessionID, us); err == nil {
			u.srv.hub.publish("sessions")
		}
	}()
}
