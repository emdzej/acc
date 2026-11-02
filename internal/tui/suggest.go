package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// suggester completes prompt input from a fixed set of choices, e.g. group
// paths. Matching is case-insensitive on any part of the choice, so "api"
// finds "platform/api"; prefix matches rank first, then segment starts.
type suggester struct {
	all    []string
	strict bool   // only a listed choice is accepted
	noun   string // what the choices are, for "no matching …" (default group)
	shown  []string
	cur    int
}

func newSuggester(choices []string, strict bool) *suggester {
	s := &suggester{all: choices, strict: strict}
	s.filter("")
	return s
}

func (s *suggester) filter(q string) {
	q = strings.ToLower(strings.TrimSpace(q))
	type hit struct {
		v    string
		rank int
	}
	var hits []hit
	for _, c := range s.all {
		l := strings.ToLower(c)
		switch {
		case q == "" || strings.HasPrefix(l, q):
			hits = append(hits, hit{c, 0})
		case strings.Contains(l, "/"+q):
			hits = append(hits, hit{c, 1})
		case strings.Contains(l, q):
			hits = append(hits, hit{c, 2})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return a.rank - b.rank })
	s.shown = s.shown[:0]
	for _, h := range hits {
		s.shown = append(s.shown, h.v)
	}
	s.cur = min(s.cur, max(0, len(s.shown)-1))
}

func (s *suggester) move(d int) {
	if n := len(s.shown); n > 0 {
		s.cur = (s.cur + d + n) % n
	}
}

func (s *suggester) selected() string {
	if s.cur < len(s.shown) {
		return s.shown[s.cur]
	}
	return ""
}

// resolve turns what was typed into a choice: an exact match wins, else the
// highlighted suggestion. ok is false when a strict prompt has no match.
func (s *suggester) resolve(typed string) (string, bool) {
	typed = strings.TrimSpace(typed)
	if i := slices.IndexFunc(s.all, func(c string) bool { return strings.EqualFold(c, typed) }); i >= 0 {
		return s.all[i], true
	}
	if sel := s.selected(); sel != "" && (s.strict || typed == "") {
		return sel, true
	}
	return typed, !s.strict && typed != ""
}

// view renders the suggestions for the key bar, highlighted one first in focus.
func (s *suggester) view(w int) string {
	key := lipgloss.NewStyle().Bold(true).Foreground(colFocus)
	out := " " + key.Render("Tab") + stDim.Render(" complete  ") + key.Render("↑↓") + stDim.Render(" choose  │ ")
	if len(s.shown) == 0 {
		msg := "no match"
		if s.strict {
			msg = "no matching " + cmp.Or(s.noun, "group")
		}
		return fit(out+stErr.Render(msg), w)
	}
	// Start the window at the highlighted item if it would scroll off.
	start := 0
	for lipgloss.Width(out+strings.Join(s.shown[start:s.cur+1], "  ")) > w-2 && start < s.cur {
		start++
	}
	if start > 0 {
		out += stDim.Render("… ")
	}
	for i := start; i < len(s.shown); i++ {
		item := s.shown[i]
		st := stDim
		if i == s.cur {
			st = lipgloss.NewStyle().Foreground(colFocus).Bold(true).Reverse(true)
			item = " " + item + " "
		}
		next := out + st.Render(item) + "  "
		if lipgloss.Width(next) > w-2 {
			out += stDim.Render("…")
			break
		}
		out = next
	}
	return fit(out, w)
}
