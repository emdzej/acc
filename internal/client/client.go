// Package client is a thin HTTP client for the ACC daemon API.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// FromEnv builds a client from $ACC_SERVER / $ACC_TOKEN or the local defaults.
func FromEnv(timeout time.Duration) (*Client, error) {
	tok, err := config.Token()
	if err != nil {
		return nil, fmt.Errorf("no ACC token (is `acc serve` running? set ACC_TOKEN for remote): %w", err)
	}
	return &Client{BaseURL: config.ServerURL(), Token: tok, HTTP: &http.Client{Timeout: timeout}}, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("acc: %s", e.Error)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) PostEvent(ctx context.Context, ev model.Event) (model.EventResponse, error) {
	var out model.EventResponse
	return out, c.do(ctx, http.MethodPost, "/api/v1/events", ev, &out)
}

func (c *Client) Sessions(ctx context.Context) ([]model.Session, error) {
	var out []model.Session
	return out, c.do(ctx, http.MethodGet, "/api/v1/sessions", nil, &out)
}

// RenameSession sets the session's title in ACC; "" reverts to the agent's title.
func (c *Client) RenameSession(ctx context.Context, sessionID, title string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/sessions/"+url.PathEscape(sessionID), model.SessionPatch{Title: &title}, nil)
}

// SetSessionColor sets a session's own colour; "" reverts to its group colour.
func (c *Client) SetSessionColor(ctx context.Context, sessionID, color string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/sessions/"+url.PathEscape(sessionID), model.SessionPatch{Color: &color}, nil)
}

func (c *Client) Events(ctx context.Context, sessionID string, limit int) ([]model.EventSummary, error) {
	var out []model.EventSummary
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/sessions/%s/events?limit=%d", url.PathEscape(sessionID), limit), nil, &out)
}

func (c *Client) Messages(ctx context.Context, sessionID string, limit int) ([]model.Message, error) {
	var out []model.Message
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/sessions/%s/messages?limit=%d", url.PathEscape(sessionID), limit), nil, &out)
}

func (c *Client) Latest(ctx context.Context, sessionID, agentID string) (transcript.Latest, error) {
	var out transcript.Latest
	q := url.Values{"agent": {agentID}}
	return out, c.do(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/latest?"+q.Encode(), nil, &out)
}

// Transcript returns entries after offset; offset < 0 starts from the tail.
func (c *Client) Transcript(ctx context.Context, sessionID, agentID string, offset int64) (transcript.Page, error) {
	var out transcript.Page
	q := url.Values{"agent": {agentID}, "offset": {fmt.Sprint(offset)}}
	return out, c.do(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/transcript?"+q.Encode(), nil, &out)
}

func (c *Client) PostStatus(ctx context.Context, r model.StatusReport) error {
	return c.do(ctx, http.MethodPost, "/api/v1/status", r, nil)
}

func (c *Client) Plan(ctx context.Context) ([]model.PlanUsage, error) {
	var out []model.PlanUsage
	return out, c.do(ctx, http.MethodGet, "/api/v1/plan", nil, &out)
}

func (c *Client) Groups(ctx context.Context) ([]model.Group, error) {
	var out []model.Group
	return out, c.do(ctx, http.MethodGet, "/api/v1/groups", nil, &out)
}

func (c *Client) UpsertGroup(ctx context.Context, name, color string) (model.Group, error) {
	var out model.Group
	return out, c.do(ctx, http.MethodPost, "/api/v1/groups", model.GroupRequest{Name: name, Color: color}, &out)
}

func (c *Client) DeleteGroup(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/groups/"+url.PathEscape(name), nil, nil)
}

func (c *Client) Assign(ctx context.Context, sessionID, group string, remove bool) error {
	return c.do(ctx, http.MethodPost, "/api/v1/assign", model.AssignRequest{SessionID: sessionID, Group: group, Remove: remove}, nil)
}

func (c *Client) Send(ctx context.Context, req model.SendRequest) (model.SendResponse, error) {
	var out model.SendResponse
	return out, c.do(ctx, http.MethodPost, "/api/v1/messages", req, &out)
}

// Claim takes a session's queued messages for immediate delivery over a channel.
func (c *Client) Claim(ctx context.Context, sessionID string) ([]model.Message, error) {
	var out model.EventResponse
	err := c.do(ctx, "POST", "/api/v1/sessions/"+url.PathEscape(sessionID)+"/claim", nil, &out)
	return out.Messages, err
}

// Stream follows the daemon's change feed, calling fn with each event name
// ("hello" on connect). It returns when the connection drops or ctx ends.
func (c *Client) Stream(ctx context.Context, fn func(topic string)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/api/v1/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	hc := *c.HTTP
	hc.Timeout = 0 // the stream is long-lived; ctx bounds it
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acc: stream: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if topic, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			fn(topic)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// Launch opens a terminal running a new agent session.
func (c *Client) Launch(ctx context.Context, req model.LaunchRequest) (model.LaunchResponse, error) {
	var out model.LaunchResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/sessions", req, &out)
	return out, err
}

// Type types text plus Enter into a session ACC started (slash commands).
func (c *Client) Type(ctx context.Context, sessionID string, req model.TypeRequest) error {
	return c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/type", req, nil)
}

// Attach brings a session ACC started back on screen.
func (c *Client) Attach(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/attach", nil, nil)
}

// Resume brings a session back on screen, or reopens it in a new window.
// The session may be one ACC doesn't list yet, from History.
func (c *Client) Resume(ctx context.Context, sessionID string, req model.ResumeRequest) (model.ResumeResponse, error) {
	var out model.ResumeResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/resume", req, &out)
	return out, err
}

// Track adds a session from History to ACC, as ended.
func (c *Client) Track(ctx context.Context, sessionID string) (model.Session, error) {
	var out model.Session
	err := c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/track", nil, &out)
	return out, err
}

// Forget removes a session from ACC; the agent's records stay.
func (c *Client) Forget(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/sessions/"+url.PathEscape(sessionID), nil, nil)
}

// ForgetEnded removes every ended session from ACC.
func (c *Client) ForgetEnded(ctx context.Context) (int, error) {
	var out model.ForgetResponse
	err := c.do(ctx, http.MethodDelete, "/api/v1/sessions?status=ended", nil, &out)
	return out.Forgotten, err
}

// History lists the sessions the agents have on record, newest first.
func (c *Client) History(ctx context.Context, limit int) ([]model.PastSession, error) {
	var out []model.PastSession
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/history?limit=%d", limit), nil, &out)
	return out, err
}
