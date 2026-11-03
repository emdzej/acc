package tui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/emdzej/acc/internal/agent"
	"github.com/emdzej/acc/internal/model"
)

// histDetailLines is the height of the History dialog's details pane.
const histDetailLines = 8

// histDialog is the History popup: a filter, the past sessions matching it,
// and the details of the highlighted one.
type histDialog struct {
	all   []model.PastSession
	shown []model.PastSession
	cur   int
	listH int // list rows last rendered, for paging
}

// filter keeps the sessions whose title, directory, id or agent contain
// every typed word.
func (h *histDialog) filter(q string) {
	words := strings.Fields(strings.ToLower(q))
	h.shown = nil
	for _, p := range h.all {
		hay := strings.ToLower(p.Title + " " + p.Cwd + " " + p.ID + " " + p.Agent)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			h.shown = append(h.shown, p)
		}
	}
	h.cur = 0
}

func (h *histDialog) move(d int) {
	if n := len(h.shown); n > 0 {
		h.cur = max(0, min(n-1, h.cur+d))
	}
}

func (h *histDialog) selected() *model.PastSession {
	if h.cur < len(h.shown) {
		return &h.shown[h.cur]
	}
	return nil
}

// startHistory opens the History dialog: every session the agents have on
// record, newest first.
func (m Model) startHistory(past []model.PastSession) (tea.Model, tea.Cmd) {
	if len(past) == 0 {
		m.setFlash("", fmt.Errorf("the agents have no sessions on record"))
		return m, nil
	}
	mm, cmd := m.startInput(inputHistory, "filter › ", "")
	nm := mm.(Model)
	nm.input.Placeholder = "title, directory or id"
	nm.hist = &histDialog{all: past}
	nm.hist.filter("")
	return nm, cmd
}

// liveInACC is the session ACC lists for p, if it hasn't ended.
func (m Model) liveInACC(p *model.PastSession) *model.Session {
	if s := m.findSession(p.ID); s != nil && s.Status != model.StatusEnded {
		return s
	}
	return nil
}

func (m Model) closeHistory() Model {
	m.mode, m.hist = inputNone, nil
	m.input.Placeholder = ""
	m.input.Blur()
	return m
}

func (m Model) updateHistory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	h := m.hist
	switch k.String() {
	case "esc":
		return m.closeHistory(), nil
	case "up", "ctrl+p":
		h.move(-1)
		return m, nil
	case "down", "ctrl+n":
		h.move(1)
		return m, nil
	case "pgup":
		h.move(-max(1, h.listH-1))
		return m, nil
	case "pgdown":
		h.move(max(1, h.listH-1))
		return m, nil
	case "enter":
		p := h.selected()
		if p == nil {
			m.setFlash("", fmt.Errorf("no session matches %q", strings.TrimSpace(m.input.Value())))
			return m, nil
		}
		id, running := p.ID, p.Running
		m = m.closeHistory()
		switch {
		case m.liveInACC(p) != nil: // resuming would start a second process
			return m, m.jumpTo(id)
		case running:
			m.openWhenListed = id
			return m, m.track(id)
		}
		m.openWhenListed = id
		return m, m.resume(id)
	case "ctrl+a":
		p := h.selected()
		if p == nil {
			return m, nil
		}
		id := p.ID
		m = m.closeHistory()
		if m.findSession(id) != nil {
			return m, m.jumpTo(id)
		}
		m.openWhenListed = id
		return m, m.track(id)
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	if m.input.Value() != before {
		h.filter(m.input.Value())
	}
	return m, cmd
}

// enterAction is what Enter does to the highlighted past session.
func (m Model) enterAction(p *model.PastSession) string {
	switch {
	case p == nil:
		return ""
	case m.liveInACC(p) != nil:
		return "open"
	case p.Running:
		return "add & open"
	}
	return "resume"
}

func (m Model) histKeys() [][2]string {
	keys := [][2]string{{"↑↓", "choose"}, {"PgUp/PgDn", "page"}}
	p := m.hist.selected()
	if a := m.enterAction(p); a != "" {
		keys = append(keys, [2]string{"⏎", a})
	}
	if p != nil && m.findSession(p.ID) == nil && !p.Running {
		keys = append(keys, [2]string{"^A", "add to ACC only"})
	}
	return append(keys, [2]string{"Esc", "close"})
}

// viewHistory draws the History dialog centred in a w×h area.
func (m Model) viewHistory(w, h int) string {
	hd := m.hist
	dw, dh := min(w, 130), h
	if w >= 70 {
		dw = min(w-4, 130)
	}
	iw := dw - 2
	listH := max(1, dh-2-2-1-histDetailLines) // borders, filter + rule, rule above details
	hd.listH = listH

	count := fmt.Sprintf("%d of %d", len(hd.shown), len(hd.all))
	var body []string
	body = append(body, fit(" "+m.input.View(), iw-lipgloss.Width(count)-1)+stDim.Render(count+" "))
	rule := stDim.Render(strings.Repeat("─", iw))
	body = append(body, rule)

	off := scrollOff(hd.cur, len(hd.shown), listH)
	for i := off; i < off+listH; i++ {
		if i >= len(hd.shown) {
			if i == 0 {
				body = append(body, stDim.Render("  no matching sessions"))
				continue
			}
			body = append(body, "")
			continue
		}
		body = append(body, m.histRow(&hd.shown[i], iw, i == hd.cur))
	}
	body = append(body, rule)
	body = append(body, m.histDetails(hd.selected(), iw)...)

	right := stDim.Render("every agent, newest first")
	b := box("History", right, body, dw, dh, true)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b)
}

func (m Model) histRow(ps *model.PastSession, w int, sel bool) string {
	mark, mst := "  ", stDim
	switch {
	case ps.Running:
		mark, mst = "● ", stOK
	case m.findSession(ps.ID) != nil:
		mark, mst = "◆ ", lipgloss.NewStyle().Foreground(colFocus)
	}
	title := stBold
	if !sel {
		title = lipgloss.NewStyle()
	}
	dir := clipRunes(filepath.Base(ps.Cwd), 28)
	right := []part{p(dir, stDim), p("  "+agentTag(ps.Agent), stDim), p(fmt.Sprintf("  %4s ", age(ps.LastActive)), stDim)}
	return row(w, sel, true, []part{p(" "+mark, mst), p(cmp.Or(ps.Title, "(untitled)"), title)}, right)
}

func agentTag(id string) string { return fmt.Sprintf("%-8s", agent.Get(id).Short) }

// histDetails describes a past session in full.
func (m Model) histDetails(ps *model.PastSession, w int) []string {
	if ps == nil {
		return nil
	}
	label := func(s string) string { return stDim.Render(fmt.Sprintf(" %-12s", s)) }
	var out []string
	title := wrap(cmp.Or(ps.Title, "(untitled)"), w-1)
	if len(title) > 2 {
		title = append(title[:1], clipRunes(strings.Join(title[1:], " "), w-1))
	}
	for _, t := range title {
		out = append(out, " "+stBold.Render(t))
	}
	out = append(out, label("directory")+tildeHome(ps.Cwd))
	out = append(out, label("agent")+ps.Agent+stDim.Render("   id ")+ps.ID)
	out = append(out, label("started")+when(ps.StartedAt)+stDim.Render("   last active ")+when(ps.LastActive))
	var size []string
	if ps.Usage != nil {
		size = append(size, usageLong(ps.Usage, m.showCost))
	}
	if ps.Size > 0 {
		size = append(size, "transcript "+bytesHuman(ps.Size))
	}
	if len(size) > 0 {
		out = append(out, label(map[bool]string{true: "usage", false: "size"}[ps.Usage != nil])+strings.Join(size, stDim.Render(" · ")))
	}
	state := stDim.Render("not in ACC")
	if s := m.findSession(ps.ID); s != nil {
		state = "in ACC, " + statusStyle(s.Status).Render(s.Status)
	}
	if ps.Running {
		state = stOK.Render("running") + stDim.Render(" in a terminal · ") + state
	}
	out = append(out, label("state")+state)
	if ps.TranscriptPath != "" {
		out = append(out, label("transcript")+stDim.Render(tildeHome(ps.TranscriptPath)))
	}
	return out
}

func when(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04") + stDim.Render(" ("+age(ms)+" ago)")
}

func bytesHuman(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func tildeHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
