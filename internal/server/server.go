// Package server exposes the ACC HTTP API, live event stream and embedded web UI.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emdzej/acc/internal/agent"
	"github.com/emdzej/acc/internal/history"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/store"
	"github.com/emdzej/acc/internal/transcript"
	"github.com/emdzej/acc/web"
)

type Server struct {
	store *store.Store
	token string
	log   *slog.Logger
	hub   *hub
	usage *usageRefresher
	chans *channels
	hist  *history.Index
	acc   Access
	fails *failures
}

func New(st *store.Store, token string, log *slog.Logger) *Server {
	s := &Server{store: st, token: token, log: log, hub: newHub(), chans: newChannels(), hist: history.NewIndex(), fails: newFailures()}
	s.usage = newUsageRefresher(s)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/events", s.postEvent)
	mux.HandleFunc("GET /api/v1/agents", s.listAgents)
	mux.HandleFunc("POST /api/v1/agents/opencode/events", s.postOpenCode)
	mux.HandleFunc("GET /api/v1/sessions", s.listSessions)
	mux.HandleFunc("PATCH /api/v1/sessions/{id}", s.patchSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", s.sessionEvents)
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", s.sessionMessages)
	mux.HandleFunc("GET /api/v1/sessions/{id}/latest", s.sessionLatest)
	mux.HandleFunc("GET /api/v1/sessions/{id}/transcript", s.sessionTranscript)
	mux.HandleFunc("POST /api/v1/sessions/{id}/claim", s.claim)
	mux.HandleFunc("POST /api/v1/sessions", s.launchSession)
	mux.HandleFunc("POST /api/v1/sessions/{id}/type", s.typeHandler)
	mux.HandleFunc("POST /api/v1/sessions/{id}/attach", s.attach)
	mux.HandleFunc("POST /api/v1/sessions/{id}/resume", s.resume)
	mux.HandleFunc("POST /api/v1/sessions/{id}/open", s.openDir)
	mux.HandleFunc("POST /api/v1/sessions/{id}/track", s.trackSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.forgetSession)
	mux.HandleFunc("DELETE /api/v1/sessions", s.forgetEnded)
	mux.HandleFunc("GET /api/v1/history", s.listHistory)
	mux.HandleFunc("GET /api/v1/groups", s.listGroups)
	mux.HandleFunc("POST /api/v1/groups", s.upsertGroup)
	mux.HandleFunc("DELETE /api/v1/groups/{path...}", s.deleteGroup)
	mux.HandleFunc("POST /api/v1/assign", s.assign)
	mux.HandleFunc("POST /api/v1/messages", s.send)
	mux.HandleFunc("GET /api/v1/stream", s.stream)
	mux.HandleFunc("POST /api/v1/status", s.postStatus)
	mux.HandleFunc("GET /api/v1/plan", s.planUsage)

	mux.HandleFunc("POST /login", s.login)

	static, _ := fs.Sub(web.FS, "dist")
	mux.Handle("GET /", http.FileServerFS(static))

	return s.guard(s.auth(mux))
}

func (s *Server) postEvent(w http.ResponseWriter, r *http.Request) {
	var ev model.Event
	if !decode(w, r, &ev) {
		return
	}
	msgs, err := s.store.RecordEvent(r.Context(), ev)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.hub.publish("sessions")
	s.usage.observe(ev)
	writeJSON(w, http.StatusOK, model.EventResponse{Messages: msgs})
}

func (s *Server) postStatus(w http.ResponseWriter, r *http.Request) {
	var rep model.StatusReport
	if !decode(w, r, &rep) {
		return
	}
	changed, err := s.store.RecordStatus(r.Context(), rep, time.Now().UnixMilli())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if changed {
		s.hub.publish("sessions")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) planUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.PlanUsage(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []model.Session{}
	}
	for i := range out {
		out[i].Channel = s.chans.live(out[i].ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) patchSession(w http.ResponseWriter, r *http.Request) {
	var p model.SessionPatch
	if !decode(w, r, &p) {
		return
	}
	id, err := s.store.ResolveSession(r.Context(), r.PathValue("id"))
	if err == nil && p.Color != nil {
		err = s.store.SetSessionColor(r.Context(), id, *p.Color)
	}
	if err == nil && p.Title != nil {
		err = s.store.SetSessionTitle(r.Context(), id, *p.Title)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	s.repaint(r.Context())
	s.hub.publish("sessions")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.RecentEvents(r.Context(), r.PathValue("id"), limitParam(r, 100, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) sessionMessages(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.Messages(r.Context(), r.PathValue("id"), limitParam(r, 50, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// sessionLatest is the default detail view: the last tool call/output, message or prompt.
func (s *Server) sessionLatest(w http.ResponseWriter, r *http.Request) {
	name, ts, payload, err := s.store.LatestPayload(r.Context(), r.PathValue("id"), r.URL.Query().Get("agent"), transcript.LatestEvents)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, transcript.Latest{})
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, transcript.FromPayload(name, ts, payload))
}

// sessionTranscript returns transcript entries after ?offset (default: tail).
// Clients follow live by passing back the returned offset.
func (s *Server) sessionTranscript(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.TranscriptPath(r.Context(), r.PathValue("id"), r.URL.Query().Get("agent"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	if path == "" {
		writeErr(w, http.StatusNotFound, errors.New("session has no transcript"))
		return
	}
	offset := int64(-1)
	if v := r.URL.Query().Get("offset"); v != "" {
		if offset, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	page, err := transcript.Read(path, offset)
	switch {
	case errors.Is(err, transcript.ErrForbidden):
		writeErr(w, http.StatusForbidden, err)
	case errors.Is(err, fs.ErrNotExist):
		writeErr(w, http.StatusNotFound, errors.New("transcript not written yet"))
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, page)
	}
}

func limitParam(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, max)
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListGroups(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) upsertGroup(w http.ResponseWriter, r *http.Request) {
	var req model.GroupRequest
	if !decode(w, r, &req) {
		return
	}
	g, err := s.store.UpsertGroup(r.Context(), req.Name, req.Color)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.repaint(r.Context())
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGroup(r.Context(), r.PathValue("path")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	s.repaint(r.Context())
	s.hub.publish("sessions")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) assign(w http.ResponseWriter, r *http.Request) {
	var req model.AssignRequest
	if !decode(w, r, &req) {
		return
	}
	id, err := s.store.ResolveSession(r.Context(), req.SessionID)
	if err == nil {
		err = s.store.Assign(r.Context(), id, req.Group, req.Remove)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	s.repaint(r.Context())
	s.hub.publish("sessions")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	var req model.SendRequest
	if !decode(w, r, &req) {
		return
	}
	if isSlash(req.Body) {
		s.sendSlash(w, r, req)
		return
	}
	// Every send is an instruction to an agent that can run code: always audit it.
	msgs, err := s.store.Enqueue(r.Context(), req, "local")
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	for _, m := range msgs {
		s.log.Info("message queued", "audit", true, "id", m.ID, "session", m.SessionID, "sender", m.Sender, "bytes", len(m.Body))
	}
	s.hub.publish("messages") // wakes channel servers (acc mcp) to claim
	s.hub.publish("sessions")
	writeJSON(w, http.StatusOK, model.SendResponse{Queued: msgs})
}

// isSlash: a single-line "/command" is typed into the terminal, because
// channel and hook messages are prompts and can't run slash commands.
func isSlash(body string) bool {
	b := strings.TrimSpace(body)
	return len(b) > 1 && b[0] == '/' && b[1] != '/' && b[1] != ' ' && !strings.Contains(b, "\n")
}

func (s *Server) sendSlash(w http.ResponseWriter, r *http.Request, req model.SendRequest) {
	ctx := r.Context()
	ids, err := s.store.Targets(ctx, req)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	resp := model.SendResponse{Queued: []model.Message{}, Skipped: map[string]string{}}
	for _, id := range ids {
		// OpenCode runs slash commands through its API, so its plugin takes
		// them from the queue like any message; no terminal needed.
		if ag, _ := s.store.SessionAgent(ctx, id); agent.Get(ag).QueueSlash {
			msgs, err := s.store.Enqueue(ctx, model.SendRequest{SessionID: id, Body: strings.TrimSpace(req.Body)}, "local")
			if err != nil {
				resp.Skipped[id] = err.Error()
				continue
			}
			resp.Queued = append(resp.Queued, msgs...)
			s.hub.publish("messages")
			continue
		}
		if err := s.typeInto(ctx, id, strings.TrimSpace(req.Body), false); err != nil {
			resp.Skipped[id] = err.Error()
			continue
		}
		resp.Typed = append(resp.Typed, id)
	}
	if len(resp.Typed) == 0 && len(resp.Queued) == 0 && len(ids) == 1 {
		writeErr(w, http.StatusBadRequest, errors.New(resp.Skipped[ids[0]]))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// stream is a Server-Sent Events feed of change notifications; clients refetch on each.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case topic := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", topic)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func decodeRaw(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return false
	}
	return true
}

func statusOf(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// hub fans change notifications out to SSE subscribers, dropping when a client is slow.
type hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func newHub() *hub { return &hub{subs: map[chan string]struct{}{}} }

func (h *hub) subscribe() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *hub) publish(topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- topic:
		default:
		}
	}
}

// listAgents describes the agents ACC supports and what it can do with each.
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	out := []model.AgentInfo{}
	for _, a := range agent.All() {
		out = append(out, a.Info())
	}
	writeJSON(w, http.StatusOK, out)
}
