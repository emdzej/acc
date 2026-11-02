// Package plan forecasts plan-limit windows: at the current pace, will the
// window run out before it resets?
package plan

import (
	"strings"
	"time"

	"github.com/emdzej/acc/internal/model"
)

// Sample is one observation of a window's usage.
type Sample struct {
	TS       int64 // unix ms
	UsedPct  float64
	ResetsAt int64 // identifies the window instance: a reset starts a new one
}

// Length is a window's duration, or 0 when unknown.
func Length(key string) time.Duration {
	switch {
	case key == "five_hour":
		return 5 * time.Hour
	case strings.HasPrefix(key, "seven_day"):
		return 7 * 24 * time.Hour
	}
	return 0
}

// lookback is how far back "current pace" looks, and minSpan the least
// history that makes a pace worth trusting, per window length.
func lookback(l time.Duration) (back, minSpan time.Duration) {
	if l <= 5*time.Hour {
		return 45 * time.Minute, 10 * time.Minute
	}
	return 6 * time.Hour, time.Hour
}

// Forecast fills w's projection from its samples (any order, any windows).
//
// The pace is the usage rate over the recent lookback, so a burst of work
// shows up quickly and an idle stretch calms the forecast. With too little
// history it falls back to the average since the window opened.
func Forecast(w *model.PlanWindow, samples []Sample, now time.Time) {
	l := Length(w.Key)
	if l == 0 || w.ResetsAt == 0 {
		return
	}
	nowMs := now.UnixMilli()
	remaining := w.ResetsAt - nowMs
	if remaining <= 0 {
		return
	}
	back, minSpan := lookback(l)

	var rate float64 // percent per ms
	basis := ""
	oldest := Sample{}
	for _, s := range samples {
		if !sameWindow(s.ResetsAt, w.ResetsAt) || s.TS < nowMs-back.Milliseconds() || s.TS > nowMs {
			continue
		}
		if oldest.TS == 0 || s.TS < oldest.TS {
			oldest = s
		}
	}
	if oldest.TS != 0 && nowMs-oldest.TS >= minSpan.Milliseconds() {
		rate = max(0, w.UsedPct-oldest.UsedPct) / float64(nowMs-oldest.TS)
		basis = model.PaceRecent
	} else if elapsed := nowMs - (w.ResetsAt - l.Milliseconds()); elapsed >= minSpan.Milliseconds() {
		rate = w.UsedPct / float64(elapsed)
		basis = model.PaceWindow
	} else {
		return // window just opened: nothing to go on
	}

	projected := w.UsedPct + rate*float64(remaining)
	w.ProjectedPct = &projected
	w.Pace = basis
	switch {
	case w.UsedPct >= 100:
		w.LimitAt = nowMs
	case rate > 0 && projected >= 100:
		w.LimitAt = nowMs + int64((100-w.UsedPct)/rate)
	}
}

// sameWindow tolerates small drift in a reported reset time; a real reset
// moves it by the window's length.
func sameWindow(a, b int64) bool {
	d := a - b
	return d > -5*time.Minute.Milliseconds() && d < 5*time.Minute.Milliseconds()
}
