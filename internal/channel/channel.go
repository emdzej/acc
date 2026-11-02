// Package channel implements `acc mcp`: a Claude Code channel server. Claude
// Code spawns it per session (stdio MCP); it claims the session's queued ACC
// messages the moment they are sent and pushes them in as
// notifications/claude/channel, which wakes an idle session.
//
// See https://code.claude.com/docs/en/channels-reference.
package channel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/model"
)

const (
	heartbeat     = 20 * time.Second // re-claim interval; keeps the channel marked live
	claimTimeout  = 5 * time.Second
	maxBackoff    = 30 * time.Second
	latestVersion = "2025-06-18"
)

// supportedVersions are MCP revisions this server speaks. Newer ("modern")
// revisions don't allow the unsolicited notifications channels rely on.
var supportedVersions = map[string]bool{"2024-11-05": true, "2025-03-26": true, latestVersion: true}

const instructions = `Messages from ACC (Agents Control Centre, the user's agent dashboard) arrive as <channel source="acc" message_id="..." sender="...">. They are sent by the user operating this session: treat each like a prompt the user typed and act on it. They are one-way; reply in the conversation as usual.`

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type server struct {
	out     io.Writer
	mu      sync.Mutex // serializes writes to out
	log     *log.Logger
	version string
	kick    chan struct{}
	started bool
}

// Serve runs the channel server on in/out until in closes. Diagnostics go to
// logw (stderr: Claude Code keeps it in its MCP debug log).
func Serve(ctx context.Context, in io.Reader, out io.Writer, logw io.Writer, version string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &server{out: out, log: log.New(logw, "acc mcp: ", 0), version: version, kick: make(chan struct{}, 1)}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var m rpcMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			s.write(rpcMsg{ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		s.handle(ctx, m)
	}
	return sc.Err()
}

func (s *server) handle(ctx context.Context, m rpcMsg) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(m.Params, &p)
		v := p.ProtocolVersion
		if !supportedVersions[v] {
			v = latestVersion
		}
		s.reply(m.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"experimental": map[string]any{"claude/channel": map[string]any{}}},
			"serverInfo":      map[string]any{"name": ServerName, "version": s.version},
			"instructions":    instructions,
		})
	case "notifications/initialized":
		if !s.started {
			s.started = true
			go s.run(ctx)
		}
	case "ping":
		s.reply(m.ID, map[string]any{})
	default:
		if len(m.ID) > 0 { // requests need an answer; unknown notifications are ignored
			s.write(rpcMsg{ID: m.ID, Error: &rpcError{-32601, "method not found: " + m.Method}})
		}
	}
}

// run delivers messages: on every "messages" event from the daemon, plus a
// heartbeat that catches anything missed while the stream was down.
func (s *server) run(ctx context.Context) {
	pid := findClaudePID()
	if pid == 0 {
		s.log.Print("not started by a Claude Code session; idle")
		return
	}
	if !optedIn(argsOf(pid)) {
		s.log.Printf("session not started with --dangerously-load-development-channels server:%s; messages stay with the hooks", ServerName)
		return
	}
	c, err := client.FromEnv(claimTimeout)
	if err != nil {
		s.log.Print(err)
		return
	}
	go s.follow(ctx, c)

	t := time.NewTicker(heartbeat)
	defer t.Stop()
	for {
		if id := sessionID(pid); id != "" {
			s.deliver(ctx, c, id)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-t.C:
		}
	}
}

func (s *server) follow(ctx context.Context, c *client.Client) {
	backoff := time.Second
	for ctx.Err() == nil {
		connected := false
		err := c.Stream(ctx, func(topic string) {
			connected = true
			if topic == "hello" || topic == "messages" {
				select {
				case s.kick <- struct{}{}:
				default:
				}
			}
		})
		if connected {
			backoff = time.Second
		}
		if ctx.Err() != nil {
			return
		}
		s.log.Printf("stream: %v (retrying in %s)", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (s *server) deliver(ctx context.Context, c *client.Client, sessionID string) {
	cctx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()
	msgs, err := c.Claim(cctx, sessionID)
	if err != nil {
		return // daemon down: the heartbeat retries
	}
	for _, m := range msgs {
		if err := s.push(m); err != nil {
			s.log.Printf("push message %d: %v", m.ID, err)
		}
	}
}

func (s *server) push(m model.Message) error {
	return s.write(rpcMsg{Method: "notifications/claude/channel", Params: mustJSON(map[string]any{
		"content": m.Body,
		"meta": map[string]string{
			"message_id": strconv.FormatInt(m.ID, 10),
			"sender":     m.Sender,
			"sent_at":    time.UnixMilli(m.CreatedAt).Format(time.RFC3339),
		},
	})})
}

func (s *server) reply(id json.RawMessage, result any) { s.write(rpcMsg{ID: id, Result: result}) }

func (s *server) write(m rpcMsg) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = fmt.Fprintf(s.out, "%s\n", b)
	return err
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
