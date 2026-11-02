package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/emdzej/acc/internal/model"
)

// channelTTL is how long a channel counts as live after its last claim;
// channel servers re-claim (heartbeat) well within it.
const channelTTL = 60 * time.Second

// channels tracks sessions with a live channel server. In memory only: after
// a daemon restart, channels re-announce on their next heartbeat.
type channels struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newChannels() *channels { return &channels{seen: map[string]time.Time{}} }

// touch records a claim; it reports whether the channel just became live.
func (c *channels) touch(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	was := time.Since(c.seen[id]) < channelTTL
	c.seen[id] = time.Now()
	return !was
}

func (c *channels) live(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.seen[id]) < channelTTL
}

// claim is called by a session's channel server: it hands over the session's
// queued messages, which the channel pushes into the session right away.
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, err := s.store.ClaimPending(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, m := range msgs {
		s.log.Info("message delivered", "audit", true, "id", m.ID, "session", m.SessionID, "via", "channel")
	}
	if s.chans.touch(id) || len(msgs) > 0 {
		s.hub.publish("sessions")
	}
	writeJSON(w, http.StatusOK, model.EventResponse{Messages: msgs})
}
