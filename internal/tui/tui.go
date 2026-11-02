// Package tui is the full-screen terminal dashboard: a sidebar with groups and
// a session tree (subagents nested under their session), a panel for the
// opened session, and a status line plus function-key bar at the bottom.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

const (
	refreshEvery  = time.Second
	maxEntries    = 2000 // transcript entries kept in memory while tracking
	flashDuration = 4 * time.Second
)

// palette is what the colour key cycles through. Sessions also get "none",
// which reverts them to their group colour.
var palette = []string{"#61afef", "#98c379", "#e5c07b", "#c678dd", "#e06c75", "#56b6c2", "#d19a66", "#abb2bf"}

type focusArea int

const (
	focusSessions focusArea = iota // the tree: groups, sessions, subagents
	focusDetail
)

type inputMode int

const (
	inputNone inputMode = iota
	inputSend
	inputAssign
	inputUnassign
	inputNewGroup
	inputHexColor
	inputConfirmDelete
	inputRename
	inputLaunchDir
	inputLaunchTitle
	inputLaunchPrompt
	inputLaunchTmux
	inputJump
)

// Options configures the dashboard.
type Options struct {
	// Embedded means this process hosts the server, which stops on quit.
	Embedded bool
}

// treeRow is a row in the tree: a group, a session in it (or at the root
// when ungrouped), or one of a session's subagents. A session in several
// groups appears under each of them.
type treeRow struct {
	g      *model.Group    // group row (s == nil), or the group a session row sits under
	s      *model.Session  // session or subagent row
	a      *model.Subagent // subagent row
	depth  int
	count  int  // group rows: sessions in the subtree
	hasKid bool // group rows: has subgroups or sessions
}

func (r treeRow) isGroup() bool { return r.s == nil }

// key identifies a row across refreshes, including which group a session is shown under.
func (r treeRow) key() string {
	k := ""
	if r.g != nil {
		k = fmt.Sprintf("g%d", r.g.ID)
	}
	if r.s != nil {
		k += "/" + r.s.ID
	}
	if r.a != nil {
		k += "/" + r.a.ID
	}
	return k
}

// target identifies what the session panel shows: a session, or a subagent of it.
type target struct {
	sessionID string
	agentID   string
}

type Model struct {
	c    *client.Client
	opts Options
	w, h int

	focus    focusArea
	sessions []model.Session
	groups   []model.Group
	plan     []model.PlanUsage
	err      error

	rows      []treeRow
	cur       int
	expanded  map[string]bool // sessions showing their subagents
	collapsed map[int64]bool  // groups hiding their children
	hideEnded bool
	showCost  bool
	selKey    string // selected row's key; survives refreshes that reorder rows

	open      *target
	latest    transcript.Latest
	messages  []model.Message
	detailErr error

	tracking   bool
	entries    []transcript.Entry
	tOffset    int64
	tInflight  bool
	scroll     int // lines scrolled up from the bottom of the session panel; 0 = follow
	detailRows int // content lines last rendered, for page sizes

	mode    inputMode
	input   textinput.Model
	launch  model.LaunchRequest // new-session draft, filled across inputs
	sugg    *suggester          // completions for the current prompt, if any
	jump    map[string]string   // jump prompt: suggestion label -> session id
	dark    bool                // terminal has a dark background (for tints)
	flash   string
	flashOK bool
	flashAt time.Time
	help    bool
}

func New(c *client.Client, opts Options) Model {
	ti := textinput.New()
	ti.CharLimit = 4000
	p := loadPrefs()
	return Model{c: c, opts: opts, input: ti, dark: lipgloss.HasDarkBackground(), expanded: map[string]bool{}, collapsed: map[int64]bool{},
		focus: focusSessions, showCost: p.ShowCost, hideEnded: p.HideEnded}
}

func Run(c *client.Client, opts Options) error {
	_, err := tea.NewProgram(New(c, opts), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// ---- messages ---------------------------------------------------------------

type listMsg struct {
	sessions []model.Session
	groups   []model.Group
	plan     []model.PlanUsage
	err      error
}

type detailMsg struct {
	t        target
	latest   *transcript.Latest
	messages []model.Message
	page     *transcript.Page
	reqOff   int64
	err      error
}

type tickMsg struct{}

type actionMsg struct {
	ok  string
	err error
}

func tick() tea.Cmd { return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} }) }

func (m Model) Init() tea.Cmd { return tea.Batch(m.fetchList(), tick()) }

func (m Model) fetchList() tea.Cmd {
	c := m.c
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s, err := c.Sessions(ctx)
		if err != nil {
			return listMsg{err: err}
		}
		g, err := c.Groups(ctx)
		plan, _ := c.Plan(ctx) // optional: older daemons or no status line feed yet
		return listMsg{sessions: s, groups: g, plan: plan, err: err}
	}
}

// fetchDetail loads the opened session's messages plus either the latest
// activity or, when tracking, the transcript entries after tOffset.
func (m *Model) fetchDetail() tea.Cmd {
	if m.open == nil || (m.tracking && m.tInflight) {
		return nil
	}
	t, c, tracking, off := *m.open, m.c, m.tracking, m.tOffset
	if tracking {
		m.tInflight = true
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out := detailMsg{t: t, reqOff: off}
		var err error
		if t.agentID == "" {
			if out.messages, err = c.Messages(ctx, t.sessionID, 20); err != nil {
				out.err = err
				return out
			}
		}
		if tracking {
			p, err := c.Transcript(ctx, t.sessionID, t.agentID, off)
			out.page, out.err = &p, err
		} else {
			l, err := c.Latest(ctx, t.sessionID, t.agentID)
			out.latest, out.err = &l, err
		}
		return out
	}
}

// ---- update -----------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.flash != "" && time.Since(m.flashAt) > flashDuration {
			m.flash = ""
		}
		return m, tea.Batch(m.fetchList(), m.fetchDetail(), tick())

	case listMsg:
		m.err = msg.err
		if msg.err == nil {
			m.sessions, m.groups, m.plan = msg.sessions, msg.groups, msg.plan
			m.rebuildRows()
		}
		return m, nil

	case detailMsg:
		if m.open == nil || msg.t != *m.open {
			return m, nil // stale: user switched sessions
		}
		if msg.page != nil {
			m.tInflight = false
		}
		m.detailErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		if msg.messages != nil {
			m.messages = msg.messages
		}
		if msg.latest != nil {
			m.latest = *msg.latest
		}
		if msg.page != nil && m.tracking && msg.reqOff == m.tOffset {
			m.entries = append(m.entries, msg.page.Entries...)
			if over := len(m.entries) - maxEntries; over > 0 {
				m.entries = m.entries[over:]
			}
			m.tOffset = msg.page.Offset
		}
		return m, nil

	case actionMsg:
		m.setFlash(msg.ok, msg.err)
		return m, tea.Batch(m.fetchList(), m.fetchDetail())

	case tea.MouseMsg:
		return m.updateMouse(msg)

	case tea.KeyMsg:
		if m.mode != inputNone {
			return m.updateInput(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m *Model) setFlash(ok string, err error) {
	m.flashAt = time.Now()
	if err != nil {
		m.flash, m.flashOK = err.Error(), false
		return
	}
	m.flash, m.flashOK = ok, true
}

func (m Model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help {
		m.help = false
		return m, nil
	}
	switch k.String() {
	case "q", "f10", "ctrl+c":
		return m, tea.Quit
	case "?", "f1":
		m.help = true
	case "tab":
		m.cycleFocus(1)
	case "shift+tab":
		m.cycleFocus(-1)
	case "esc":
		if m.open != nil {
			m.closeSession()
		}
	case "m", "f2":
		return m.startSend()
	case "g", "f3":
		return m.startOnSession(inputAssign, "add to group › ")
	case "u", "f4":
		return m.startOnSession(inputUnassign, "remove from group › ")
	case "n", "f5":
		// Pre-fill the current group's path so Enter-ing a name creates a child.
		prefix := ""
		if g := m.contextGroup(); g != nil {
			prefix = g.Path + "/"
		}
		return m.startInput(inputNewGroup, "new group (path [#rrggbb]) › ", prefix)
	case "c", "f6":
		return m, m.cycleColor()
	case "C":
		return m.startHexColor()
	case "N":
		return m.startLaunch()
	case "/", "ctrl+f":
		return m.startJump()
	case "a":
		s := m.actionSession()
		if s == nil || s.Host == nil {
			m.setFlash("", fmt.Errorf("only sessions ACC started can be brought back"))
			return m, nil
		}
		c, id := m.c, s.ID
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return actionMsg{"brought back on screen", c.Attach(ctx, id)}
		}
	case "r":
		s := m.actionSession()
		if s == nil {
			m.setFlash("", fmt.Errorf("select a session first"))
			return m, nil
		}
		return m.startInput(inputRename, "rename in ACC (empty = agent's title) › ", s.ACCTitle)
	case "d", "f8":
		if g := m.selGroup(); g != nil {
			return m.startInput(inputConfirmDelete, fmt.Sprintf("delete group %q and its subgroups? (y/N) › ", g.Path), "")
		}
		m.setFlash("", fmt.Errorf("select a group to delete"))
	case "t":
		m.toggleTracking()
		return m, m.fetchDetail()
	case "e":
		m.hideEnded = !m.hideEnded
		m.savePrefs()
		m.rebuildRows()
	case "$":
		m.showCost = !m.showCost
		m.savePrefs()
		if m.showCost {
			m.setFlash("cost shown ($ to hide)", nil)
		} else {
			m.setFlash("cost hidden ($ to show)", nil)
		}
	}

	switch m.focus {
	case focusSessions:
		switch k.String() {
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			m.move(-10)
		case "pgdown":
			m.move(10)
		case "home":
			m.move(-len(m.rows))
		case "end", "G":
			m.move(len(m.rows))
		case "right", "l":
			m.setExpanded(true)
		case "left", "h":
			r := m.currentRow()
			if r == nil {
				break
			}
			if m.isExpanded(*r) {
				m.setExpanded(false)
			} else {
				m.selectParent()
			}
		case " ":
			if r := m.currentRow(); r != nil {
				m.setExpanded(!m.isExpanded(*r))
			}
		case "enter":
			if r := m.currentRow(); r != nil && r.isGroup() {
				m.setExpanded(!m.isExpanded(*r))
				return m, nil
			}
			return m, m.openCurrent()
		}
	case focusDetail:
		page := max(1, m.detailRows-2)
		switch k.String() {
		case "up", "k":
			m.scroll++
		case "down", "j":
			m.scroll = max(0, m.scroll-1)
		case "pgup":
			m.scroll += page
		case "pgdown":
			m.scroll = max(0, m.scroll-page)
		case "end", "G":
			m.scroll = 0
		case "home":
			m.scroll = 1 << 30 // clamped when rendering
		}
	}
	return m, nil
}

func (m Model) savePrefs() { prefs{ShowCost: m.showCost, HideEnded: m.hideEnded}.save() }

func (m *Model) cycleFocus(d int) {
	areas := []focusArea{focusSessions}
	if m.open != nil {
		areas = append(areas, focusDetail)
	}
	i := slices.Index(areas, m.focus)
	m.focus = areas[(i+d+len(areas))%len(areas)]
}

func (m *Model) openCurrent() tea.Cmd {
	r := m.currentRow()
	if r == nil || r.isGroup() {
		return nil
	}
	t := target{sessionID: r.s.ID}
	if r.a != nil {
		t.agentID = r.a.ID
	}
	if m.open == nil || *m.open != t {
		m.open = &t
		m.latest, m.messages, m.detailErr = transcript.Latest{}, nil, nil
		m.resetTranscript()
	}
	m.focus = focusDetail
	return m.fetchDetail()
}

func (m *Model) closeSession() {
	m.open = nil
	m.tracking = false
	m.resetTranscript()
	m.focus = focusSessions
}

func (m *Model) toggleTracking() {
	if m.open == nil {
		m.setFlash("", fmt.Errorf("open a session first (Enter)"))
		return
	}
	m.tracking = !m.tracking
	m.resetTranscript()
	if m.tracking {
		m.setFlash("tracking transcript live (t to stop)", nil)
	}
}

func (m *Model) resetTranscript() {
	m.entries, m.tOffset, m.tInflight, m.scroll = nil, -1, false, 0
}

// ---- input prompts ------------------------------------------------------------

func (m Model) startInput(mode inputMode, prompt, value string) (tea.Model, tea.Cmd) {
	m.mode = mode
	m.sugg = nil
	m.input.Prompt = prompt
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.flash = ""
	return m, m.input.Focus()
}

// startLaunch begins the new-session dialog. The directory defaults to the
// selected session's; the session joins the group selected in the Groups panel.
func (m Model) startLaunch() (tea.Model, tea.Cmd) {
	m.launch = model.LaunchRequest{}
	if g := m.contextGroup(); g != nil {
		m.launch.Group = g.Path
	}
	dir := "~/"
	if s := m.actionSession(); s != nil && s.Cwd != "" {
		dir = s.Cwd
	}
	prompt := "new session in › "
	if m.launch.Group != "" {
		prompt = "new session (group " + m.launch.Group + ") in › "
	}
	mm, cmd := m.startInput(inputLaunchDir, prompt, dir)
	nm := mm.(Model)
	seen := map[string]bool{}
	var dirs []string
	for _, s := range m.visibleSessions() {
		if s.Cwd != "" && !seen[s.Cwd] {
			seen[s.Cwd] = true
			dirs = append(dirs, s.Cwd)
		}
	}
	nm.sugg = newSuggester(dirs, false) // any directory is fine; these are shortcuts
	nm.sugg.filter(dir)
	return nm, cmd
}

func (m Model) submitLaunch() tea.Cmd {
	c, req := m.c, m.launch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		resp, err := c.Launch(ctx, req)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{ok: fmt.Sprintf("started %s in %s: confirm the channel prompt in the new window", resp.SessionID[:8], resp.Terminal)}
	}
}

// actionSession is the session acted on: the opened one when the panel has
// focus, else the selected row's session.
func (m Model) actionSession() *model.Session {
	if m.focus == focusDetail && m.open != nil {
		return m.findSession(m.open.sessionID)
	}
	if r := m.currentRow(); r != nil {
		return r.s // nil on group rows
	}
	return nil
}

func (m Model) startOnSession(mode inputMode, prompt string) (tea.Model, tea.Cmd) {
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session first"))
		return m, nil
	}
	// Offer only valid groups: ones the session isn't in yet, or the ones it's in.
	in := map[int64]bool{}
	for _, g := range s.Groups {
		in[g.ID] = true
	}
	var choices []string
	for _, g := range m.groups {
		if (mode == inputUnassign) == in[g.ID] {
			choices = append(choices, g.Path)
		}
	}
	if len(choices) == 0 {
		if mode == inputUnassign {
			m.setFlash("", fmt.Errorf("%s isn't in any group", s.Title))
		} else {
			m.setFlash("", fmt.Errorf("no group to add %s to: create one with n", s.Title))
		}
		return m, nil
	}
	mm, cmd := m.startInput(mode, prompt, "")
	nm := mm.(Model)
	nm.sugg = newSuggester(choices, true)
	return nm, cmd
}

func (m Model) startSend() (tea.Model, tea.Cmd) {
	if g := m.selGroup(); g != nil {
		return m.startInput(inputSend, fmt.Sprintf("message group %s (incl. subgroups) › ", g.Path), "")
	}
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session first"))
		return m, nil
	}
	return m.startInput(inputSend, fmt.Sprintf("message %s › ", s.Title), "")
}

func (m Model) startHexColor() (tea.Model, tea.Cmd) {
	if g := m.selGroup(); g != nil {
		return m.startInput(inputHexColor, fmt.Sprintf("colour for group %s (#rrggbb, empty = inherit) › ", g.Path), g.Color)
	}
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session or group first"))
		return m, nil
	}
	return m.startInput(inputHexColor, fmt.Sprintf("colour for %s (#rrggbb, empty = group colour) › ", s.Title), s.Color)
}

func (m Model) updateInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.sugg != nil {
		switch k.Type {
		case tea.KeyTab:
			if sel := m.sugg.selected(); sel != "" {
				m.input.SetValue(sel)
				m.input.CursorEnd()
				m.sugg.filter(sel)
			}
			return m, nil
		case tea.KeyUp, tea.KeyShiftTab:
			m.sugg.move(-1)
			return m, nil
		case tea.KeyDown:
			m.sugg.move(1)
			return m, nil
		case tea.KeyEnter:
			val, ok := m.sugg.resolve(m.input.Value())
			if !ok {
				if m.mode == inputJump {
					m.setFlash("", fmt.Errorf("no session matches %q", strings.TrimSpace(m.input.Value())))
				} else {
					m.setFlash("", fmt.Errorf("no group %q: pick one of the suggestions (n creates a group)", strings.TrimSpace(m.input.Value())))
				}
				return m, nil // keep the prompt open to fix it
			}
			if m.mode == inputJump {
				id := m.jump[val]
				m.mode, m.sugg, m.jump = inputNone, nil, nil
				m.input.Blur()
				return m, m.jumpTo(id)
			}
			m.input.SetValue(val)
		}
	}
	switch k.Type {
	case tea.KeyEsc:
		m.mode = inputNone
		m.sugg, m.jump = nil, nil
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		m.sugg = nil
		val := strings.TrimSpace(m.input.Value())
		mode := m.mode
		m.mode = inputNone
		m.input.Blur()
		switch mode { // the new-session dialog asks dir → title → first prompt
		case inputLaunchDir:
			if val == "" {
				return m, nil
			}
			m.launch.Cwd = val
			return m.startInput(inputLaunchTitle, "title (optional) › ", "")
		case inputLaunchTitle:
			m.launch.Title = val
			return m.startInput(inputLaunchPrompt, "first prompt (optional) › ", "")
		case inputLaunchPrompt:
			m.launch.Prompt = val
			return m.startInput(inputLaunchTmux, "host in tmux? (y/n, Enter = config default) › ", "")
		case inputLaunchTmux:
			switch strings.ToLower(val) {
			case "y", "yes":
				t := true
				m.launch.Tmux = &t
			case "n", "no":
				f := false
				m.launch.Tmux = &f
			}
			return m, m.submitLaunch()
		}
		if val == "" && mode != inputHexColor && mode != inputRename {
			return m, nil
		}
		return m, m.submit(mode, val)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.sugg != nil {
		m.sugg.filter(m.input.Value())
	}
	return m, cmd
}

func (m Model) submit(mode inputMode, val string) tea.Cmd {
	c := m.c
	var group model.Group
	sg := m.selGroup()
	onGroup := sg != nil
	if onGroup {
		group = *sg
	}
	var sid, title string
	if s := m.actionSession(); s != nil {
		sid, title = s.ID, s.Title
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		switch mode {
		case inputSend:
			req := model.SendRequest{SessionID: sid, Body: val}
			if onGroup {
				req = model.SendRequest{Group: group.Path, Body: val}
			}
			resp, err := c.Send(ctx, req)
			if err != nil {
				return actionMsg{err: err}
			}
			if len(resp.Typed) > 0 || len(resp.Skipped) > 0 {
				msg := fmt.Sprintf("typed into %d session(s)", len(resp.Typed))
				if len(resp.Skipped) > 0 {
					msg += fmt.Sprintf(", %d skipped (not started by ACC, or showing a dialog)", len(resp.Skipped))
				}
				return actionMsg{ok: msg}
			}
			return actionMsg{fmt.Sprintf("sent to %d session(s): live channels deliver now, others when the session next acts", len(resp.Queued)), nil}
		case inputAssign:
			return actionMsg{title + " → " + val, c.Assign(ctx, sid, val, false)}
		case inputUnassign:
			return actionMsg{title + " removed from " + val, c.Assign(ctx, sid, val, true)}
		case inputNewGroup:
			path, color, hasColor := strings.Cut(val, " #")
			path = strings.Trim(strings.TrimSpace(path), "/")
			switch {
			case hasColor:
				color = "#" + strings.TrimSpace(color)
			case !strings.Contains(path, "/"):
				color = palette[len(path)%len(palette)] // roots get a colour; children inherit
			}
			_, err := c.UpsertGroup(ctx, path, color)
			return actionMsg{"group " + path + " saved", err}
		case inputHexColor:
			if onGroup {
				_, err := c.UpsertGroup(ctx, group.Path, val)
				return actionMsg{"group colour set", err}
			}
			return actionMsg{"session colour set", c.SetSessionColor(ctx, sid, val)}
		case inputRename:
			msg := "renamed to " + val
			if val == "" {
				msg = "ACC title cleared"
			}
			return actionMsg{msg, c.RenameSession(ctx, sid, val)}
		case inputConfirmDelete:
			if !strings.EqualFold(val, "y") || !onGroup {
				return actionMsg{"cancelled", nil}
			}
			return actionMsg{"group " + group.Path + " deleted", c.DeleteGroup(ctx, group.Path)}
		}
		return nil
	}
}

// cycleColor steps the focused group's or session's colour through the palette.
func (m Model) cycleColor() tea.Cmd {
	c := m.c
	if g := m.selGroup(); g != nil {
		group := *g
		next := palette[(slices.Index(palette, group.Color)+1)%len(palette)]
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.UpsertGroup(ctx, group.Path, next)
			return actionMsg{"group " + group.Path + " → " + next, err}
		}
	}
	s := m.actionSession()
	if s == nil {
		return nil
	}
	cycle := append([]string{""}, palette...) // "" = use group colour
	next := cycle[(slices.Index(cycle, s.Color)+1)%len(cycle)]
	id := s.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		label := next
		if label == "" {
			label = "group colour"
		}
		return actionMsg{"session colour → " + label, c.SetSessionColor(ctx, id, next)}
	}
}

// ---- list state ---------------------------------------------------------------

// subtree returns the IDs of group id and all its descendants.
func (m Model) subtree(id int64) map[int64]bool {
	out := map[int64]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, g := range m.groups {
			if out[g.ParentID] && !out[g.ID] {
				out[g.ID], changed = true, true
			}
		}
	}
	return out
}

// visibleSessions applies the ended toggle, ordered by start time, newest
// first, so rows don't jump as sessions work.
func (m Model) visibleSessions() []*model.Session {
	var out []*model.Session
	for i := range m.sessions {
		if s := &m.sessions[i]; !(m.hideEnded && s.Status == model.StatusEnded) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// rebuildRows flattens the tree: each group, then its subgroups and its
// sessions (with subagents when expanded); ungrouped sessions at the root.
func (m *Model) rebuildRows() {
	visible := m.visibleSessions()
	kids := map[int64][]*model.Group{}
	for i := range m.groups {
		g := &m.groups[i]
		kids[g.ParentID] = append(kids[g.ParentID], g)
	}
	direct := map[int64][]*model.Session{}
	var ungrouped []*model.Session
	for _, s := range visible {
		if len(s.Groups) == 0 {
			ungrouped = append(ungrouped, s)
		}
		for _, g := range s.Groups {
			direct[g.ID] = append(direct[g.ID], s)
		}
	}
	var count func(id int64) int
	count = func(id int64) int {
		seen := map[string]bool{}
		in := m.subtree(id)
		for gid := range in {
			for _, s := range direct[gid] {
				seen[s.ID] = true
			}
		}
		return len(seen)
	}

	m.rows = m.rows[:0]
	addSession := func(s *model.Session, g *model.Group, depth int) {
		m.rows = append(m.rows, treeRow{g: g, s: s, depth: depth})
		if m.expanded[s.ID] {
			for j := range s.Subagents {
				m.rows = append(m.rows, treeRow{g: g, s: s, a: &s.Subagents[j], depth: depth + 1})
			}
		}
	}
	var addGroup func(g *model.Group, depth int)
	addGroup = func(g *model.Group, depth int) {
		m.rows = append(m.rows, treeRow{g: g, depth: depth, count: count(g.ID), hasKid: len(kids[g.ID]) > 0 || len(direct[g.ID]) > 0})
		if m.collapsed[g.ID] {
			return
		}
		for _, c := range kids[g.ID] {
			addGroup(c, depth+1)
		}
		for _, s := range direct[g.ID] {
			addSession(s, g, depth+1)
		}
	}
	for _, g := range kids[0] {
		addGroup(g, 0)
	}
	for _, s := range ungrouped {
		addSession(s, nil, 0)
	}

	m.cur = min(m.cur, max(0, len(m.rows)-1))
	for i, r := range m.rows {
		if r.key() == m.selKey {
			m.cur = i
			break
		}
	}
	if r := m.currentRow(); r != nil {
		m.selKey = r.key()
	}
}

func rowTarget(r treeRow) target {
	t := target{sessionID: r.s.ID}
	if r.a != nil {
		t.agentID = r.a.ID
	}
	return t
}

func (m *Model) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.cur = max(0, min(len(m.rows)-1, m.cur+d))
	m.selKey = m.rows[m.cur].key()
}

func (m Model) currentRow() *treeRow {
	if m.cur >= 0 && m.cur < len(m.rows) {
		return &m.rows[m.cur]
	}
	return nil
}

// selGroup is the selected group row's group (nil for session rows).
func (m Model) selGroup() *model.Group {
	if r := m.currentRow(); m.focus == focusSessions && r != nil && r.isGroup() {
		return r.g
	}
	return nil
}

// contextGroup is the group the cursor is in: the selected group, or the
// group a selected session is shown under.
func (m Model) contextGroup() *model.Group {
	if r := m.currentRow(); m.focus == focusSessions && r != nil {
		return r.g
	}
	return nil
}

func (m Model) isExpanded(r treeRow) bool {
	switch {
	case r.isGroup():
		return r.hasKid && !m.collapsed[r.g.ID]
	case r.a == nil:
		return m.expanded[r.s.ID]
	}
	return false
}

// setExpanded opens or closes the selected group or session.
func (m *Model) setExpanded(open bool) {
	r := m.currentRow()
	if r == nil {
		return
	}
	switch {
	case r.isGroup():
		if r.hasKid {
			m.collapsed[r.g.ID] = !open
		}
	case r.a == nil && len(r.s.Subagents) > 0:
		m.expanded[r.s.ID] = open
	default:
		return
	}
	m.rebuildRows()
}

// selectParent moves to the row's parent: a subagent's session, a session's
// or subgroup's group.
func (m *Model) selectParent() {
	r := m.currentRow()
	if r == nil || r.depth == 0 {
		return
	}
	for i := m.cur - 1; i >= 0; i-- {
		if m.rows[i].depth < r.depth {
			m.cur = i
			m.selKey = m.rows[i].key()
			return
		}
	}
}

func (m Model) findSession(id string) *model.Session {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			return &m.sessions[i]
		}
	}
	return nil
}

func (m Model) findSubagent(t target) *model.Subagent {
	if s := m.findSession(t.sessionID); s != nil {
		for i := range s.Subagents {
			if s.Subagents[i].ID == t.agentID {
				return &s.Subagents[i]
			}
		}
	}
	return nil
}

// ---- mouse ------------------------------------------------------------------

func (m Model) updateMouse(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != inputNone || m.help {
		return m, nil
	}
	l := m.layout()
	wheel := 0
	switch ev.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	case tea.MouseButtonLeft:
		if ev.Action != tea.MouseActionPress {
			return m, nil
		}
	default:
		return m, nil
	}

	switch {
	case l.sessions.contains(ev.X, ev.Y):
		m.focus = focusSessions
		if wheel != 0 {
			m.move(wheel)
			return m, nil
		}
		if i := ev.Y - l.sessions.y - 1 + l.sessOff; i >= 0 && i < len(m.rows) {
			again := i == m.cur
			m.cur = i
			m.selKey = m.rows[i].key()
			if again { // click a selected row to open it (or toggle a group)
				if m.rows[i].isGroup() {
					m.setExpanded(!m.isExpanded(m.rows[i]))
					return m, nil
				}
				return m, m.openCurrent()
			}
		}
	case l.detail.contains(ev.X, ev.Y) && m.open != nil:
		m.focus = focusDetail
		if wheel != 0 {
			m.scroll = max(0, m.scroll-wheel*3)
		}
	}
	return m, nil
}

// startJump opens the jump-to-session prompt: type any part of a title,
// group, directory or id, and Enter opens the highlighted session.
func (m Model) startJump() (tea.Model, tea.Cmd) {
	var labels []string
	ids := map[string]string{}
	for _, s := range m.sessionsByActivity() {
		label := s.Title
		for _, g := range s.Groups {
			label += " · " + g.Path
		}
		if s.Cwd != "" && filepath.Base(s.Cwd) != s.Title {
			label += " · " + filepath.Base(s.Cwd)
		}
		label += " · " + model.ShortID(s.ID)
		if s.Status == model.StatusEnded {
			label += " (ended)"
		}
		labels = append(labels, label)
		ids[label] = s.ID
	}
	if len(labels) == 0 {
		m.setFlash("", fmt.Errorf("no sessions yet"))
		return m, nil
	}
	mm, cmd := m.startInput(inputJump, "jump to › ", "")
	nm := mm.(Model)
	nm.sugg = newSuggester(labels, true)
	nm.sugg.noun = "session"
	nm.jump = ids
	return nm, cmd
}

// sessionsByActivity lists sessions most recently active first, ended last.
func (m Model) sessionsByActivity() []*model.Session {
	out := make([]*model.Session, len(m.sessions))
	for i := range m.sessions {
		out[i] = &m.sessions[i]
	}
	sort.SliceStable(out, func(i, j int) bool {
		ei, ej := out[i].Status == model.StatusEnded, out[j].Status == model.StatusEnded
		if ei != ej {
			return ej
		}
		return out[i].LastSeen > out[j].LastSeen
	})
	return out
}

// jumpTo selects a session in the tree, unfolding its group and showing
// ended sessions if needed, and opens it.
func (m *Model) jumpTo(id string) tea.Cmd {
	var s *model.Session
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			s = &m.sessions[i]
		}
	}
	if s == nil {
		return nil
	}
	if s.Status == model.StatusEnded && m.hideEnded {
		m.hideEnded = false
	}
	parent := map[int64]int64{}
	for _, g := range m.groups {
		parent[g.ID] = g.ParentID
	}
	for _, g := range s.Groups {
		for gid := g.ID; gid != 0; gid = parent[gid] {
			delete(m.collapsed, gid)
		}
	}
	m.focus = focusSessions
	m.rebuildRows()
	for i, r := range m.rows {
		if r.s != nil && r.a == nil && r.s.ID == id {
			m.cur, m.selKey = i, r.key()
			break
		}
	}
	return m.openCurrent()
}
