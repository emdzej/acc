package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/emdzej/acc/internal/agent"
	"github.com/emdzej/acc/internal/hook"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

const narrowWidth = 90

type layout struct {
	sessions, detail rect
	sessOff          int
}

// layout is computed identically for View and mouse hit-testing.
func (m Model) layout() layout {
	var l layout
	ph := max(3, m.h-2) // status line + key bar
	if m.w < narrowWidth {
		// One column: the session panel replaces the sidebar while focused.
		if m.open != nil && (m.focus == focusDetail || m.help) {
			l.detail = rect{0, 0, m.w, ph}
		} else {
			m.sidebar(&l, m.w, ph)
		}
	} else {
		lw := max(30, min(52, m.w*34/100))
		m.sidebar(&l, lw, ph)
		l.detail = rect{lw, 0, m.w - lw, ph}
	}
	l.sessOff = scrollOff(m.cur, len(m.rows), l.sessions.h-2)
	return l
}

func (m Model) sidebar(l *layout, w, ph int) { l.sessions = rect{0, 0, w, ph} }

// scrollOff keeps cursor visible in a window of n rows.
func scrollOff(cursor, total, n int) int {
	if n <= 0 || total <= n {
		return 0
	}
	return max(0, min(total-n, cursor-n+1))
}

func (m Model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	if m.hist != nil {
		return m.viewHistory(m.w, max(3, m.h-2)) + "\n" + m.viewStatus() + "\n" + m.viewKeys()
	}
	l := m.layout()
	var main string
	if l.sessions.w > 0 {
		main = m.viewSessions(l)
	}
	if l.detail.w > 0 {
		var d string
		if m.help {
			d = box("Help", "", helpLines(), l.detail.w, l.detail.h, true)
		} else {
			d = m.viewDetail(l.detail)
		}
		if main == "" {
			main = d
		} else {
			main = lipgloss.JoinHorizontal(lipgloss.Top, main, d)
		}
	}
	return main + "\n" + m.viewStatus() + "\n" + m.viewKeys()
}

// ---- sidebar ----------------------------------------------------------------

func (m Model) viewSessions(l layout) string {
	r := l.sessions
	inner := r.w - 2
	focused := m.focus == focusSessions
	var lines []string
	if len(m.rows) == 0 {
		lines = append(lines, stDim.Render(" No sessions yet."))
		if len(m.sessions) == 0 {
			lines = append(lines, stDim.Render(" Run `acc setup`, then start an agent."))
		}
	}
	for i := l.sessOff; i < len(m.rows) && len(lines) < r.h-2; i++ {
		tr := m.rows[i]
		sel := i == m.cur
		if tr.isGroup() {
			lines = append(lines, m.groupRow(tr, inner, sel, focused))
			continue
		}
		isOpen := m.open != nil && *m.open == rowTarget(tr)
		lines = append(lines, m.sessionRow(tr, inner, sel, focused, isOpen))
	}
	count := fmt.Sprintf("%d", len(m.visibleSessions()))
	if m.hideEnded {
		count += " · ended hidden"
	}
	return box("Sessions", stDim.Render(count), lines, r.w, r.h, focused)
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

func (m Model) groupRow(tr treeRow, w int, sel, focused bool) string {
	g := tr.g
	exp := "  "
	if tr.hasKid {
		exp = "▾ "
		if m.collapsed[g.ID] {
			exp = "▸ "
		}
	}
	dot := "● " // own colour
	if g.Color == "" {
		dot = "◦ " // inherited (or none)
	}
	left := []part{
		p(" "+indent(tr.depth)+exp, stDim),
		p(dot, swatch(g.Display)),
		p(g.Name, stBold),
	}
	return row(w, sel, focused, left, []part{p(fmt.Sprintf("%d ", tr.count), stDim)})
}

func (m Model) sessionRow(tr treeRow, w int, sel, focused, isOpen bool) string {
	mark := " "
	if isOpen {
		mark = "›"
	}
	if tr.a != nil {
		a := tr.a
		label := a.Type
		if label == "" {
			label = "subagent"
		}
		detail := a.LastTool
		left := []part{
			p(mark+indent(tr.depth)+" └ ", stDim),
			p("● ", statusStyle(a.Status)),
			p(label+" ", lipgloss.NewStyle()),
			p(detail, stDim),
		}
		return row(w, sel, focused, left, []part{p(age(a.LastSeen)+" ", stDim)})
	}
	s := tr.s
	exp := "  "
	if n := len(s.Subagents); n > 0 {
		exp = "▸ "
		if m.expanded[s.ID] {
			exp = "▾ "
		}
	}
	titleSt := lipgloss.NewStyle()
	if s.Status == model.StatusEnded {
		titleSt = stDim
	}
	// The status dot becomes the attention badge when the agent waits on you,
	// so it sits where the eye scans first.
	dot := p("● ", statusStyle(s.Status))
	if icon, st := badge(s.Attention); icon != "" {
		dot = p(icon+" ", st)
		if s.Attention != model.AttentionInput {
			titleSt = titleSt.Bold(true)
		}
	}
	left := []part{
		p(mark+indent(tr.depth), stBold),
		p("┃", swatch(s.DisplayColor())),
		p(exp, stDim),
		dot,
		p(s.Title, titleSt),
	}
	var right []part
	if s.ContextPct != nil && *s.ContextPct >= 70 && s.Status != model.StatusEnded {
		right = append(right, p(fmt.Sprintf("ctx %d%% ", int(*s.ContextPct)), pctStyle(*s.ContextPct)))
	}
	if running := runningSubagents(s); running > 0 {
		right = append(right, p(fmt.Sprintf("⑂%d ", running), stWarn))
	}
	if s.PendingCount > 0 {
		right = append(right, p(fmt.Sprintf("✉%d ", s.PendingCount), statusStyle(model.StatusWaiting)))
	}
	if u := s.Usage; u != nil && w >= 36 && m.showCost && u.CostUSD > 0 {
		right = append(right, p(money(u.CostUSD)+" ", stDim))
	}
	right = append(right, p(age(s.LastSeen)+" ", stDim))
	// The title wins over indicators: drop them least-important first
	// (ctx, subagents, queued, cost) until it has room. Age always stays.
	const minTitle = 14
	fixed := 0
	for _, x := range left[:len(left)-1] {
		fixed += lipgloss.Width(x.text)
	}
	for len(right) > 1 && fixed+min(minTitle, lipgloss.Width(s.Title))+partsWidth(right) > w {
		right = right[1:]
	}
	return row(w, sel, focused, left, right)
}

func partsWidth(ps []part) int {
	n := 0
	for _, x := range ps {
		n += lipgloss.Width(x.text)
	}
	return n
}

func runningSubagents(s *model.Session) int {
	n := 0
	for _, a := range s.Subagents {
		if a.Status == model.SubagentRunning {
			n++
		}
	}
	return n
}

// ---- session panel ------------------------------------------------------------

func (m Model) viewDetail(r rect) string {
	focused := m.focus == focusDetail
	inner := r.w - 2
	if m.open == nil {
		return box("Session", "", m.overviewLines(inner), r.w, r.h, false)
	}
	s := m.findSession(m.open.sessionID)
	if s == nil {
		return box("Session", "", []string{stDim.Render(" Session no longer listed.")}, r.w, r.h, focused)
	}
	var sub *model.Subagent
	if m.open.agentID != "" {
		sub = m.findSubagent(*m.open)
	}

	// Header.
	title := s.Title + " · " + s.Agent
	status := s.Status
	if sub != nil {
		title = "↳ " + firstNonEmpty(sub.Type, "subagent") + " · subagent of " + s.Title
		status = sub.Status
	}
	right := statusStyle(status).Render("● " + status)
	var head []string
	if sub != nil {
		head = append(head, " "+stDim.Render("agent ")+sub.ID+stDim.Render("   started ")+clock(sub.StartedAt)+stDim.Render("   seen ")+age(sub.LastSeen))
		if sub.Result != "" && !m.tracking {
			head = append(head, "")
		}
	} else {
		head = append(head, " "+stDim.Render("id ")+s.ID+stDim.Render("   cwd ")+s.Cwd)
		var g strings.Builder
		g.WriteString(" " + stDim.Render("groups "))
		if len(s.Groups) == 0 {
			g.WriteString(stDim.Render("none"))
		}
		for _, x := range s.Groups {
			g.WriteString(swatch(x.Display).Render("● ") + x.Path + "  ")
		}
		own := stDim.Render("inherits")
		if s.Color != "" {
			own = swatch(s.Color).Render("██ " + s.Color)
		}
		g.WriteString(stDim.Render("  colour ") + own)
		head = append(head, g.String())
		meta := " " + stDim.Render("started ") + clock(s.StartedAt) + stDim.Render("   seen ") + age(s.LastSeen)
		if n := len(s.Subagents); n > 0 {
			meta += stDim.Render("   subagents ") + fmt.Sprintf("%d", n)
			if run := runningSubagents(s); run > 0 {
				meta += stWarn.Render(fmt.Sprintf(" (%d running)", run))
			}
		}
		if s.Host != nil {
			meta += stDim.Render("   host ") + s.Host.Kind + stDim.Render(" (a: bring back, /commands work)")
		}
		ag := agent.Get(s.Agent)
		if s.Status == model.StatusEnded && ag.CanResume() {
			meta += stDim.Render("   a: resume")
		}
		if s.Channel {
			meta += stOK.Render("   ⚡ live channel")
		} else if s.Status != model.StatusEnded && ag.Unconnected != "" {
			meta += stDim.Render("   " + ag.Unconnected)
		}
		if s.PendingCount > 0 {
			meta += statusStyle(model.StatusWaiting).Render(fmt.Sprintf("   ✉ %d queued", s.PendingCount))
		}
		head = append(head, meta)
		if g := s.Goal; g != nil {
			head = append(head, goalLine(g))
		}
		if s.Model != "" || s.ContextPct != nil {
			line := " " + stDim.Render("model ") + firstNonEmpty(s.Model, "-")
			if s.ContextPct != nil {
				line += stDim.Render("   context ") + pctStyle(*s.ContextPct).Render(fmt.Sprintf("%d%%", int(*s.ContextPct)))
				if s.ContextSize > 0 {
					line += stDim.Render(" of " + tokens(s.ContextSize))
				}
				line += " " + meter(*s.ContextPct, 20)
			}
			head = append(head, line)
		}
		if u := s.Usage; u != nil {
			head = append(head, " "+stDim.Render("usage ")+usageLong(u, m.showCost))
		}
		if icon, st := badge(s.Attention); icon != "" {
			line := " " + st.Render(icon+" "+attentionBadge[s.Attention].label)
			if s.AttentionSince > 0 {
				line += stDim.Render(" for " + age(s.AttentionSince))
			}
			if s.AttentionDetail != "" {
				line += "  " + strings.Join(strings.Fields(s.AttentionDetail), " ")
			}
			head = append(head, line)
		}
	}
	// Tint the info block with the session's colour, so the open session is
	// recognisable at a glance; a solid bar marks the edge.
	if c := s.DisplayColor(); c != "" {
		base, mix := "#1b1d23", 0.20
		if !m.dark {
			base, mix = "#ffffff", 0.16
		}
		bg := blend(c, base, mix)
		bar := lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Background(lipgloss.Color(bg)).Render("▌")
		for i, l := range head {
			head[i] = tint(bar+strings.TrimPrefix(l, " "), inner, bg)
		}
	}
	head = append(head, "")

	// Section header.
	section := stBold.Render(" Latest") + stDim.Render("   t: track transcript live")
	if m.tracking {
		section = stOK.Render(" ● Transcript · live") + stDim.Render("   t: stop · ↑↓/PgUp/PgDn scroll · End: follow")
	}
	head = append(head, section)

	// Footer: messages sent to the main session.
	var foot []string
	if sub == nil && len(m.messages) > 0 {
		foot = append(foot, "", stBold.Render(" Messages"))
		for i := min(len(m.messages), 4) - 1; i >= 0; i-- {
			msg := m.messages[i]
			icon, st, suffix := "✓ ", stOK, ""
			if msg.DeliveredAt == 0 {
				icon, st, suffix = "⧗ ", stWarn, stDim.Render("  (waiting: delivered when the session next acts)")
			}
			body := strings.Join(strings.Fields(msg.Body), " ")
			foot = append(foot, " "+st.Render(icon)+stDim.Render(clock(msg.CreatedAt)+" ")+body+suffix)
		}
	}

	// Body: scrollable, anchored to the bottom so live output follows.
	bodyH := max(1, r.h-2-len(head)-len(foot))
	var body []string
	if m.detailErr != nil {
		body = []string{stErr.Render(" " + m.detailErr.Error())}
	} else if m.tracking {
		body = m.transcriptLines(inner)
	} else {
		body = m.latestLines(inner, sub)
	}
	scroll := min(m.scroll, max(0, len(body)-bodyH))
	end := len(body) - scroll
	start := max(0, end-bodyH)
	visible := body[start:end]
	if scroll > 0 {
		section += stWarn.Render(fmt.Sprintf("  ↑%d", scroll))
		head[len(head)-1] = section
	}
	for len(visible) < bodyH {
		visible = append(visible, "")
	}

	lines := append(append(head, visible...), foot...)
	return box(title, right, lines, r.w, r.h, focused)
}

func (m Model) latestLines(w int, sub *model.Subagent) []string {
	l := m.latest
	ind := "   "
	var out []string
	add := func(st lipgloss.Style, s string) {
		for _, x := range wrap(s, w-len(ind)) {
			out = append(out, ind+st.Render(x))
		}
	}
	switch l.Kind {
	case "tool":
		out = append(out, " "+stWarn.Render("⚙ "+l.Tool)+stDim.Render("  "+l.Event+" · "+clock(l.TS)))
		add(lipgloss.NewStyle(), l.Input)
		if l.Output != "" || (l.Event != "PreToolUse" && l.Event != "PermissionRequest") {
			out = append(out, " "+stDim.Render("output"))
			st := stDim
			if l.Error {
				st = stErr
			}
			if l.Output == "" {
				add(stDim, "(empty)")
			} else {
				add(st, l.Output)
			}
		}
	case "message":
		out = append(out, " "+stBold.Render("assistant")+stDim.Render("  "+clock(l.TS)))
		add(lipgloss.NewStyle(), l.Text)
	case "prompt":
		label := "prompt"
		if l.Source != "" {
			label = "← " + l.Source
		}
		out = append(out, " "+lipgloss.NewStyle().Foreground(colFocus).Render(label)+stDim.Render("  "+clock(l.TS)))
		add(lipgloss.NewStyle(), l.Text)
	case "notification":
		out = append(out, " "+statusStyle(model.StatusWaiting).Render("⚠ needs attention")+stDim.Render("  "+clock(l.TS)))
		add(lipgloss.NewStyle(), l.Text)
	default:
		if sub != nil && sub.Result != "" {
			out = append(out, " "+stBold.Render("result"))
			add(lipgloss.NewStyle(), sub.Result)
		} else {
			out = append(out, stDim.Render(" No activity recorded yet."))
		}
	}
	return out
}

func (m Model) transcriptLines(w int) []string {
	if len(m.entries) == 0 {
		if m.tInflight || m.tOffset < 0 {
			return []string{stDim.Render(" Loading transcript…")}
		}
		return []string{stDim.Render(" Transcript is empty so far.")}
	}
	const gutter = 10 // "hh:mm:ss  "
	var out []string
	for _, e := range m.entries {
		ts := strings.Repeat(" ", gutter)
		if e.TS > 0 {
			ts = stDim.Render(time.UnixMilli(e.TS).Format("15:04:05")) + "  "
		}
		var label string
		var st lipgloss.Style
		text, maxLines := e.Text, 0
		switch e.Kind {
		case transcript.KindPrompt:
			label, st = lipgloss.NewStyle().Foreground(colFocus).Bold(true).Render("you "), lipgloss.NewStyle()
			if e.Source != "" { // e.g. an ACC message delivered over the channel
				label = lipgloss.NewStyle().Foreground(colFocus).Bold(true).Render("✉   ")
				text = "← " + e.Source + ": " + text
			}
		case transcript.KindText:
			label, st = stBold.Render("ai  "), lipgloss.NewStyle()
		case transcript.KindThinking:
			label, st, maxLines = stDim.Render("✻   "), stDim.Italic(true), 2
		case transcript.KindToolUse:
			label, st = stWarn.Render("⚙   "), stWarn
			text = e.Tool + "  " + text
		case transcript.KindToolResult:
			label, st, maxLines = stDim.Render("↳   "), stDim, 8
			if e.Error {
				st = stErr
			}
		}
		lines := wrap(text, w-gutter-5)
		if maxLines > 0 && len(lines) > maxLines {
			more := len(lines) - maxLines
			lines = append(lines[:maxLines], fmt.Sprintf("… %d more lines", more))
		}
		for i, x := range lines {
			if i == 0 {
				out = append(out, " "+ts+label+st.Render(x))
			} else {
				out = append(out, " "+strings.Repeat(" ", gutter+4)+st.Render(x))
			}
		}
	}
	return out
}

func (m Model) overviewLines(w int) []string {
	counts := map[string]int{}
	for _, s := range m.sessions {
		counts[s.Status]++
	}
	out := []string{
		"",
		" " + stBold.Render("Agents Control Centre"),
		"",
	}
	for _, st := range []string{model.StatusWorking, model.StatusWaiting, model.StatusIdle, model.StatusActive, model.StatusEnded} {
		out = append(out, fmt.Sprintf("   %s %-8s %d", statusStyle(st).Render("●"), st, counts[st]))
	}
	out = append(out, "",
		stDim.Render(" Select a session and press Enter to open it."),
		stDim.Render(" → expands subagents · Tab switches panels · ? for help"))
	return out
}

// ---- bottom bars ----------------------------------------------------------------

func (m Model) viewStatus() string {
	bar := lipgloss.NewStyle().Background(colBar)
	if m.mode != inputNone && m.hist == nil {
		line := " " + m.input.View()
		if m.flash != "" && !m.flashOK && time.Since(m.flashAt) < flashDuration {
			line += "  " + stErr.Render(m.flash)
		}
		return bar.Width(m.w).Render(fit(line, m.w))
	}
	left := lipgloss.NewStyle().Background(colFocus).Foreground(lipgloss.Color("#000000")).Bold(true).Render(" ACC ")
	conn := bar.Foreground(lipgloss.Color("#98c379")).Render(" ● live ")
	if m.err != nil {
		conn = bar.Foreground(lipgloss.Color("#e06c75")).Render(" ✗ offline ")
	}
	left += conn + bar.Foreground(colDim).Render(strings.TrimPrefix(m.c.BaseURL, "http://")+" ")
	if m.opts.Embedded {
		left += bar.Foreground(lipgloss.Color("#e5c07b")).Render("◆ embedded (stops on quit) ")
	}

	counts := map[string]int{}
	for _, s := range m.sessions {
		counts[s.Status]++
	}
	var right string
	need := 0
	for _, s := range m.sessions {
		if needsYou(s.Attention) {
			need++
		}
	}
	if need > 0 {
		right += bar.Foreground(lipgloss.Color("#e06c75")).Bold(true).Render(fmt.Sprintf("⚑ %d need you ", need))
	}
	for _, pu := range m.plan {
		for _, w := range pu.Windows {
			right += bar.Foreground(pctColor(w.UsedPct)).Render(fmt.Sprintf("%s %d%%", w.Label, int(w.UsedPct)))
			if w.LimitWarning() {
				// At the current pace the window runs out before it resets.
				right += bar.Foreground(lipgloss.Color("#e06c75")).Bold(true).Render(" ⚠" + hook.FormatReset(w.LimitAt, time.Now()))
			}
			if w.ResetsAt > 0 {
				right += bar.Foreground(colDim).Render(" ↻" + hook.FormatReset(w.ResetsAt, time.Now()))
			}
			right += bar.Render("  ")
		}
		if age := time.Since(time.UnixMilli(pu.UpdatedAt)); age > 10*time.Minute {
			right += bar.Foreground(colDim).Render("(" + age.Round(time.Minute).String() + " old) ")
		}
	}
	var cost float64
	for _, s := range m.sessions {
		if s.Usage != nil {
			cost += s.Usage.CostUSD
		}
	}
	if cost > 0 && m.showCost {
		right += bar.Foreground(colDim).Render("Σ " + money(cost) + " ")
	}
	for _, st := range []string{model.StatusWorking, model.StatusWaiting, model.StatusIdle} {
		if counts[st] > 0 {
			right += bar.Foreground(statusPalette[st]).Render(fmt.Sprintf("%d %s ", counts[st], st))
		}
	}

	mid := ""
	switch {
	case m.err != nil:
		mid = bar.Foreground(lipgloss.Color("#e06c75")).Render(m.err.Error())
	case m.flash != "":
		fc := lipgloss.Color("#98c379")
		if !m.flashOK {
			fc = "#e06c75"
		}
		mid = bar.Foreground(fc).Render(m.flash)
	}
	space := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if lipgloss.Width(mid) > space-2 {
		mid = fit(mid, max(0, space-2))
	}
	gap := max(0, space-lipgloss.Width(mid))
	return fit(left+bar.Render(strings.Repeat(" ", gap/2))+mid+bar.Render(strings.Repeat(" ", gap-gap/2))+right, m.w)
}

// keyHints are the bottom bar's shortcuts for what's selected. Letters, not
// F-keys: function keys need Fn on Mac keyboards (F-keys still work as aliases).
var keyHints = map[string][][2]string{
	"group": {{"?", "help"}, {"/", "jump"}, {"⏎", "fold"}, {"m", "msg group"}, {"N", "new session"}, {"n", "subgroup"}, {"c", "colour"}, {"d", "delete"},
		{"H", "history"}, {"e", "ended"}, {"$", "cost"}, {"q", "quit"}},
	"session": {{"?", "help"}, {"/", "jump"}, {"⏎", "open"}, {"N", "new"}, {"m", "msg"}, {"g", "group"}, {"u", "ungroup"}, {"r", "rename"}, {"c", "colour"},
		{"a", "resume"}, {"o", "terminal"}, {"O", "files"}, {"x", "forget"}, {"H", "history"}, {"→", "subagents"}, {"e", "ended"}, {"$", "cost"}, {"Tab", "panel"}, {"q", "quit"}},
	"detail": {{"?", "help"}, {"⇧↑↓ [ ]", "prev/next session"}, {"t", "transcript"}, {"m", "msg"}, {"↑↓", "scroll"}, {"r", "rename"}, {"c", "colour"},
		{"$", "cost"}, {"Esc", "close"}, {"Tab", "panel"}, {"q", "quit"}},
}

func (m Model) viewKeys() string {
	if m.mode != inputNone && m.sugg != nil {
		return m.sugg.view(m.w)
	}
	key := lipgloss.NewStyle().Bold(true).Foreground(colFocus)
	hints := "session"
	if m.focus == focusDetail {
		hints = "detail"
	} else if m.selGroup() != nil {
		hints = "group"
	}
	list := keyHints[hints]
	if m.hist != nil {
		list = m.histKeys()
	}
	var sb strings.Builder
	for _, k := range list {
		item := key.Render(k[0]) + " " + stDim.Render(k[1]) + "   "
		if lipgloss.Width(sb.String())+lipgloss.Width(item) > m.w {
			break
		}
		sb.WriteString(item)
	}
	return fit(" "+sb.String(), m.w)
}

func helpLines() []string {
	rows := [][2]string{
		{"Tab / Shift-Tab", "switch between the tree and the session panel"},
		{"↑↓ j k, PgUp/PgDn", "move / scroll"},
		{"Enter / click", "open a session or subagent; fold or unfold a group"},
		{"→ ← / Space", "expand / collapse (← on a closed row goes to its parent)"},
		{"/ (Ctrl-F)", "jump to a session: type part of its title, group, directory or id"},
		{"Shift-↑↓ / [ ]", "open the previous / next session, from either panel"},
		{"Esc", "close the session panel / cancel a prompt"},
		{"t", "toggle live transcript tracking (off by default)"},
		{"m", "message the session (on a group: all its live sessions, incl. subgroups)"},
		{"g / u", "add session to / remove from a group (path)"},
		{"n", "new group; prefilled as a child of the group you're in"},
		{"N", "new session in a terminal window (joins the group you're in)"},
		{"a", "resume: bring the session's window back, or reopen an ended session in a new one"},
		{"o / O", "open a terminal / Finder (file manager) in the session's directory"},
		{"H", "history: every session the agents have on record; resume one or add it to ACC"},
		{"x / X", "forget the session / all ended sessions (ACC only: the agent keeps them)"},
		{"m /cmd", "a message starting with / is typed into the terminal (sessions ACC started)"},
		{"5h 80% ⚠14:20", "status bar: at the current pace the 5-hour limit runs out at 14:20, before it resets (↻)"},
		{"r", "rename session in ACC (empty reverts to the agent's title)"},
		{"c / C", "cycle colour / enter #rrggbb (group or session)"},
		{"d", "delete selected group and its subgroups"},
		{"e", "hide/show ended sessions"},
		{"$", "show/hide cost (tokens stay visible)"},
		{"q", "quit"},
		{"", ""},
		{"F-keys", "F1 help · F2 msg · F3/F4 group · F5 new · F6 colour · F8 del · F10 quit"},
		{"Colours", "session's own → its group's → nearest ancestor's"},
		{"Delivery", "messages arrive when the session next acts (prompt, tool, turn end, resume)"},
	}
	out := []string{""}
	for _, r := range rows {
		out = append(out, fmt.Sprintf("  %s  %s", stBold.Render(fmt.Sprintf("%-18s", r[0])), r[1]))
	}
	return append(out, "", stDim.Render("  any key to close"))
}

// ---- formatting ---------------------------------------------------------------

// usageLong renders the session panel's usage line; money only if showCost.
func usageLong(u *model.Usage, showCost bool) string {
	var b strings.Builder
	if u.CostUSD > 0 && showCost {
		b.WriteString(stBold.Render(money(u.CostUSD)))
		if !u.CostKnown {
			b.WriteString(stDim.Render(" (partial)"))
		}
		b.WriteString("  ")
	}
	fmt.Fprintf(&b, "in %s · out %s · cache %s read / %s write",
		tokens(u.InputTokens), tokens(u.OutputTokens), tokens(u.CacheReadTokens), tokens(u.CacheWriteTokens))
	if u.LinesAdded+u.LinesRemoved > 0 {
		b.WriteString("  " + stOK.Render(fmt.Sprintf("+%d", u.LinesAdded)) + " " + stErr.Render(fmt.Sprintf("-%d", u.LinesRemoved)))
	}
	if len(u.Models) > 0 {
		b.WriteString(stDim.Render("  " + strings.Join(u.Models, ", ")))
	}
	return b.String()
}

// needsYou: attention states that block the agent until the user acts.
func needsYou(att string) bool {
	return att == model.AttentionPermission || att == model.AttentionQuestion || att == model.AttentionNotice
}

func pctColor(p float64) lipgloss.Color {
	switch {
	case p >= 80:
		return "#e06c75"
	case p >= 50:
		return "#e5c07b"
	}
	return "#98c379"
}

func pctStyle(p float64) lipgloss.Style { return lipgloss.NewStyle().Foreground(pctColor(p)) }

// meter draws a w-cell bar filled to p percent.
func meter(p float64, w int) string {
	n := max(0, min(w, int(p/100*float64(w)+0.5)))
	return pctStyle(p).Render(strings.Repeat("━", n)) + stDim.Render(strings.Repeat("─", w-n))
}

func money(v float64) string {
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func age(ms int64) string {
	if ms == 0 {
		return "-"
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func clock(ms int64) string {
	if ms == 0 {
		return "-"
	}
	t := time.UnixMilli(ms)
	if time.Since(t) > 20*time.Hour {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04:05")
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// goalLine shows the session's /goal: status, objective, progress and the
// judge's latest verdict.
func goalLine(g *model.Goal) string {
	st := stWarn
	switch g.Status {
	case model.GoalMet:
		st = stOK
	case model.GoalFailed:
		st = stErr
	case model.GoalPaused:
		st = stDim
	}
	line := " " + stDim.Render("goal ") + st.Render("◎ "+g.Status) + "  " + strings.Join(strings.Fields(g.Objective), " ")
	if g.Rounds > 0 && g.MaxRounds > 0 {
		line += stDim.Render(fmt.Sprintf("   round %d/%d", g.Rounds, g.MaxRounds))
	} else if g.Rounds > 0 {
		line += stDim.Render(fmt.Sprintf("   round %d", g.Rounds))
	}
	if g.Reason != "" {
		line += stDim.Render("   " + strings.Join(strings.Fields(g.Reason), " "))
	}
	return line
}
