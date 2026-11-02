package plan

import (
	"testing"
	"time"

	"github.com/emdzej/acc/internal/model"
)

func TestForecast(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) int64 { return now.Add(d).UnixMilli() }
	resets := ms(2 * time.Hour) // 3h into a 5h window

	// 20 points in the last 40 minutes: 0.5%/min, 120 min left → 60 + 60 = 120%.
	w := model.PlanWindow{Key: "five_hour", UsedPct: 60, ResetsAt: resets}
	Forecast(&w, []Sample{{TS: ms(-40 * time.Minute), UsedPct: 40, ResetsAt: resets}}, now)
	if w.Pace != model.PaceRecent || w.ProjectedPct == nil || int(*w.ProjectedPct+0.5) != 120 {
		t.Fatalf("recent pace: %+v", w)
	}
	if got := time.UnixMilli(w.LimitAt).Sub(now); got != 80*time.Minute { // 40% left at 0.5%/min
		t.Errorf("limit in %s, want 80m", got)
	}

	// Idle lately: no limit warning even though the window average is high.
	w = model.PlanWindow{Key: "five_hour", UsedPct: 60, ResetsAt: resets}
	Forecast(&w, []Sample{{TS: ms(-30 * time.Minute), UsedPct: 60, ResetsAt: resets}}, now)
	if w.LimitAt != 0 || *w.ProjectedPct != 60 {
		t.Errorf("idle: %+v", w)
	}

	// No history: average since the window opened (60% in 3h → 100% at reset).
	w = model.PlanWindow{Key: "five_hour", UsedPct: 60, ResetsAt: resets}
	Forecast(&w, nil, now)
	if w.Pace != model.PaceWindow || int(*w.ProjectedPct+0.5) != 100 {
		t.Errorf("window average: %+v", w)
	}

	// Samples from the previous window instance are ignored.
	w = model.PlanWindow{Key: "five_hour", UsedPct: 10, ResetsAt: resets}
	Forecast(&w, []Sample{{TS: ms(-20 * time.Minute), UsedPct: 90, ResetsAt: ms(-time.Hour)}}, now)
	if w.Pace != model.PaceWindow {
		t.Errorf("old window used: %+v", w)
	}

	// Unknown windows get no forecast.
	w = model.PlanWindow{Key: "monthly", UsedPct: 50, ResetsAt: resets}
	Forecast(&w, nil, now)
	if w.ProjectedPct != nil {
		t.Error("unknown window forecast")
	}
}
