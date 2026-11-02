package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/emdzej/acc/internal/model"
)

// Meta is session state Claude Code writes to the transcript without
// necessarily firing a hook.
type Meta struct {
	Custom, AI string      // latest /rename title and auto-generated title
	Goal       *model.Goal // latest /goal; nil when cleared or never set
	GoalSeen   bool        // a goal record was in the scanned range, so Goal is current
}

// ReadMeta returns the session's titles and goal. Claude Code re-appends
// title records often, so the tail usually has them; full=true scans the
// whole file (used once per session).
func ReadMeta(path string, full bool) (Meta, error) {
	var m Meta
	real, err := checkPath(path)
	if err != nil {
		return m, err
	}
	f, err := os.Open(real)
	if err != nil {
		return m, err
	}
	defer f.Close()
	if !full {
		if fi, err := f.Stat(); err == nil && fi.Size() > tailBytes {
			f.Seek(fi.Size()-tailBytes, io.SeekStart)
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		// Cheap pre-filter: these records are rare among large message lines.
		goal := bytes.Contains(b, []byte(`"goal_status"`))
		if !goal && !bytes.Contains(b, []byte(`-title"`)) {
			continue
		}
		var l struct {
			Type        string     `json:"type"`
			Timestamp   string     `json:"timestamp"`
			CustomTitle string     `json:"customTitle"`
			AITitle     string     `json:"aiTitle"`
			Attachment  goalStatus `json:"attachment"`
		}
		if json.Unmarshal(b, &l) != nil {
			continue // e.g. the partial first line of the tail window
		}
		switch {
		case l.Type == "custom-title":
			m.Custom = l.CustomTitle
		case l.Type == "ai-title":
			m.AI = l.AITitle
		case l.Type == "attachment" && l.Attachment.Type == "goal_status":
			m.Goal, m.GoalSeen = l.Attachment.goal(l.Timestamp), true
		}
	}
	return m, sc.Err()
}

// goalStatus is Claude Code's goal record. Setting a goal writes a sentinel
// with met=false and clearing one a sentinel with met=true; each check by
// the judge writes met (achieved), failed (impossible) or neither (keep going).
type goalStatus struct {
	Type       string `json:"type"`
	Condition  string `json:"condition"`
	Met        bool   `json:"met"`
	Failed     bool   `json:"failed"`
	Sentinel   bool   `json:"sentinel"`
	Reason     string `json:"reason"`
	Iterations int    `json:"iterations"`
}

func (a goalStatus) goal(ts string) *model.Goal {
	if a.Sentinel && a.Met {
		return nil
	}
	g := &model.Goal{Objective: a.Condition, Status: model.GoalActive, Reason: a.Reason, Rounds: a.Iterations}
	switch {
	case a.Failed:
		g.Status = model.GoalFailed
	case a.Met:
		g.Status = model.GoalMet
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		g.UpdatedAt = t.UnixMilli()
	}
	return g
}
