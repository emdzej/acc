package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/model"
)

const (
	statusPostTimeout = 500 * time.Millisecond // the status line must stay snappy
	chainTimeout      = 3 * time.Second
)

// claudeStatus is the subset of Claude Code's status line input ACC uses.
type claudeStatus struct {
	SessionID string `json:"session_id"`
	Model     struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		UsedPercentage    *float64 `json:"used_percentage"`
		ContextWindowSize int64    `json:"context_window_size"`
	} `json:"context_window"`
	RateLimits map[string]struct {
		UsedPercentage *float64        `json:"used_percentage"`
		ResetsAt       json.RawMessage `json:"resets_at"`
	} `json:"rate_limits"`
}

var windowLabels = map[string]string{"five_hour": "5h", "seven_day": "7d", "seven_day_opus": "7d opus", "seven_day_sonnet": "7d sonnet"}

// windowOrder puts the session (5h) window first, then weekly ones.
var windowOrder = map[string]int{"five_hour": 0, "seven_day": 1, "seven_day_opus": 2, "seven_day_sonnet": 3}

// RunStatusline handles Claude Code's statusLine command: it forwards plan
// limits and context fill to the daemon, then prints a status line, either
// ACC's own or, with chain, the output of the user's original command.
func RunStatusline(chain string, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return err
	}
	var st claudeStatus
	jsonErr := json.Unmarshal(raw, &st)
	rep := statusReport(st)

	// Post concurrently with the chained command so ACC adds no latency.
	posted := make(chan struct{})
	go func() {
		defer close(posted)
		if jsonErr != nil {
			return
		}
		c, err := client.FromEnv(statusPostTimeout)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), statusPostTimeout)
		defer cancel()
		c.PostStatus(ctx, rep)
	}()

	line := ""
	if chain != "" {
		line = runChain(chain, raw)
	} else if jsonErr == nil {
		line = formatStatus(rep)
	}
	<-posted
	_, err = io.WriteString(out, line)
	return err
}

func statusReport(st claudeStatus) model.StatusReport {
	rep := model.StatusReport{
		Agent:       AgentClaude,
		SessionID:   st.SessionID,
		Model:       firstNonEmpty(st.Model.DisplayName, st.Model.ID),
		ContextPct:  st.ContextWindow.UsedPercentage,
		ContextSize: st.ContextWindow.ContextWindowSize,
	}
	for key, w := range st.RateLimits {
		if w.UsedPercentage == nil {
			continue
		}
		label := windowLabels[key]
		if label == "" {
			label = strings.ReplaceAll(key, "_", " ")
		}
		rep.Plan = append(rep.Plan, model.PlanWindow{Key: key, Label: label, UsedPct: *w.UsedPercentage, ResetsAt: parseReset(w.ResetsAt)})
	}
	sort.Slice(rep.Plan, func(i, j int) bool {
		oi, iok := windowOrder[rep.Plan[i].Key]
		oj, jok := windowOrder[rep.Plan[j].Key]
		if iok != jok {
			return iok
		}
		if oi != oj {
			return oi < oj
		}
		return rep.Plan[i].Key < rep.Plan[j].Key
	})
	return rep
}

// parseReset accepts unix seconds, unix ms or an RFC 3339 string; returns unix ms.
func parseReset(raw json.RawMessage) int64 {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n < 1e12 {
			return int64(n * 1000)
		}
		return int64(n)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UnixMilli()
		}
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return parseReset(json.RawMessage(strconv.FormatInt(v, 10)))
		}
	}
	return 0
}

// formatStatus renders ACC's own status line, e.g.
// "Opus 5.5 · ctx 37% · 5h 80% ↻14:00 · 7d 42% ↻Thu 09:00".
func formatStatus(r model.StatusReport) string {
	parts := []string{}
	if r.Model != "" {
		parts = append(parts, r.Model)
	}
	if r.ContextPct != nil {
		parts = append(parts, "ctx "+colorPct(*r.ContextPct))
	}
	for _, w := range r.Plan {
		s := w.Label + " " + colorPct(w.UsedPct)
		if w.ResetsAt > 0 {
			s += " \x1b[2m↻" + FormatReset(w.ResetsAt, time.Now()) + "\x1b[0m"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " \x1b[2m·\x1b[0m ")
}

// FormatReset shows a reset time compactly: "14:00" within a day, else "Thu 09:00".
func FormatReset(ms int64, now time.Time) string {
	t := time.UnixMilli(ms).Local()
	if t.Sub(now) < 20*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

func colorPct(p float64) string {
	code := "32" // green
	switch {
	case p >= 80:
		code = "31"
	case p >= 50:
		code = "33"
	}
	return fmt.Sprintf("\x1b[%sm%d%%\x1b[0m", code, int(math.Round(p)))
}

// runChain runs the user's original status line command with the same input.
func runChain(cmd string, input []byte) string {
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Stdin = bytes.NewReader(input)
	out, _ := c.Output()
	return string(out)
}
