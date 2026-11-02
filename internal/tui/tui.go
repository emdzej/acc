// Package tui is the full-screen terminal dashboard: a sidebar with groups and
// a session tree (subagents nested under their session), a panel for the
// opened session, and a status line plus function-key bar at the bottom.
package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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
	focusGroups focusArea = iota
	focusSessions
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
)

// Options configures the dashboard.
type Options struct {
	// Embedded means this process hosts the server, which stops on quit.
	Embedded bool
}

// groupItem is a row in the groups panel: All, the group tree, then Ungrouped.
type groupItem struct {
	kind        int // itemAll | itemGroup | itemUngrouped
	group       model.Group
	count       int // sessions in this group or any descendant
	hasChildren bool
}

const (
	itemAll = iota
	itemGroup
	itemUngrouped
)

// treeRow is a row in the sessions panel: a session or one of its subagents.
type treeRow struct {
	s *model.Session
	a *model.Subagent
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

	groupCur   int
	rows       []treeRow
	sessCur    int
	sessOff    int
	expanded   map[string]bool // sessions showing their subagents
	collapsed  map[int64]bool  // groups hiding their children
	hideEnded  bool
	showCost   bool
	selectedID target // survives refreshes that reorder rows

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
	flash   string
	flashOK bool
	flashAt time.Time
	help    bool
}

func New(c *client.Client, opts Options) Model {
	ti := textinput.New()
	ti.CharLimit = 4000
	p := loadPrefs()
	return Model{c: c, opts: opts, input: ti, expanded: map[string]bool{}, collapsed: map[int64]bool{},
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
			m.groupCur = min(m.groupCur, len(m.groupItems())-1)
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
		// Pre-fill the selected group's path so Enter-ing a name creates a child.
		prefix := ""
		if it := m.currentGroupItem(); m.focus == focusGroups && it.kind == itemGroup {
			prefix = it.group.Path + "/"
		}
		return m.startInput(inputNewGroup, "new group (path [#rrggbb]) › ", prefix)
	case "c", "f6":
		return m, m.cycleColor()
	case "C":
		return m.startHexColor()
	case "d", "f8":
		if it := m.currentGroupItem(); m.focus == focusGroups && it.kind == itemGroup {
			return m.startInput(inputConfirmDelete, fmt.Sprintf("delete group %q and its subgroups? (y/N) › ", it.group.Path), "")
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
	case focusGroups:
		switch k.String() {
		case "up", "k":
			m.groupCur = max(0, m.groupCur-1)
			m.rebuildRows()
		case "down", "j":
			m.groupCur = min(len(m.groupItems())-1, m.groupCur+1)
			m.rebuildRows()
		case "right", "l":
			if it := m.currentGroupItem(); it.hasChildren {
				m.collapsed[it.group.ID] = false
			}
		case "left", "h":
			if it := m.currentGroupItem(); it.hasChildren && !m.collapsed[it.group.ID] {
				m.collapsed[it.group.ID] = true
			} else if it.kind == itemGroup && it.group.ParentID != 0 {
				m.selectGroup(it.group.ParentID)
			}
			m.rebuildRows()
		case " ":
			if it := m.currentGroupItem(); it.hasChildren {
				m.collapsed[it.group.ID] = !m.collapsed[it.group.ID]
			}
		case "enter":
			m.focus = focusSessions
		}
	case focusSessions:
		switch k.String() {
		case "up", "k":
			m.moveSession(-1)
		case "down", "j":
			m.moveSession(1)
		case "pgup":
			m.moveSession(-10)
		case "pgdown":
			m.moveSession(10)
		case "right", "l", " ":
			if r := m.currentRow(); r != nil && r.a == nil && len(r.s.Subagents) > 0 {
				m.expanded[r.s.ID] = !m.expanded[r.s.ID] || k.String() == "right" || k.String() == "l"
				m.rebuildRows()
			}
		case "left", "h":
			if r := m.currentRow(); r != nil {
				m.expanded[r.s.ID] = false
				m.selectedID = target{sessionID: r.s.ID}
				m.rebuildRows()
			}
		case "enter":
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
	areas := []focusArea{focusGroups, focusSessions}
	if m.open != nil {
		areas = append(areas, focusDetail)
	}
	i := slices.Index(areas, m.focus)
	m.focus = areas[(i+d+len(areas))%len(areas)]
}

func (m *Model) openCurrent() tea.Cmd {
	r := m.currentRow()
	if r == nil {
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
	m.input.Prompt = prompt
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.flash = ""
	return m, m.input.Focus()
}

// actionSession is the session acted on: the opened one when the panel has
// focus, else the selected row's session.
func (m Model) actionSession() *model.Session {
	if m.focus == focusDetail && m.open != nil {
		return m.findSession(m.open.sessionID)
	}
	if r := m.currentRow(); r != nil {
		return r.s
	}
	return nil
}

func (m Model) startOnSession(mode inputMode, prompt string) (tea.Model, tea.Cmd) {
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session first"))
		return m, nil
	}
	return m.startInput(mode, prompt, "")
}

func (m Model) startSend() (tea.Model, tea.Cmd) {
	if it := m.currentGroupItem(); m.focus == focusGroups && it.kind == itemGroup {
		return m.startInput(inputSend, fmt.Sprintf("message group %s (incl. subgroups) › ", it.group.Path), "")
	}
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session first"))
		return m, nil
	}
	return m.startInput(inputSend, fmt.Sprintf("message %s › ", s.Title), "")
}

func (m Model) startHexColor() (tea.Model, tea.Cmd) {
	if it := m.currentGroupItem(); m.focus == focusGroups && it.kind == itemGroup {
		return m.startInput(inputHexColor, fmt.Sprintf("colour for group %s (#rrggbb, empty = inherit) › ", it.group.Path), it.group.Color)
	}
	s := m.actionSession()
	if s == nil {
		m.setFlash("", fmt.Errorf("select a session or group first"))
		return m, nil
	}
	return m.startInput(inputHexColor, fmt.Sprintf("colour for %s (#rrggbb, empty = group colour) › ", s.Title), s.Color)
}

func (m Model) updateInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = inputNone
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		val := strings.TrimSpace(m.input.Value())
		mode := m.mode
		m.mode = inputNone
		m.input.Blur()
		if val == "" && mode != inputHexColor {
			return m, nil
		}
		return m, m.submit(mode, val)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) submit(mode inputMode, val string) tea.Cmd {
	c := m.c
	it := m.currentGroupItem()
	onGroup := m.focus == focusGroups && it.kind == itemGroup
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
				req = model.SendRequest{Group: it.group.Path, Body: val}
			}
			resp, err := c.Send(ctx, req)
			return actionMsg{fmt.Sprintf("queued for %d session(s): delivered when each next acts (prompt, tool call, turn end or resume)", len(resp.Queued)), err}
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
				_, err := c.UpsertGroup(ctx, it.group.Path, val)
				return actionMsg{"group colour set", err}
			}
			return actionMsg{"session colour set", c.SetSessionColor(ctx, sid, val)}
		case inputConfirmDelete:
			if !strings.EqualFold(val, "y") || !onGroup {
				return actionMsg{"cancelled", nil}
			}
			return actionMsg{"group " + it.group.Path + " deleted", c.DeleteGroup(ctx, it.group.Path)}
		}
		return nil
	}
}

// cycleColor steps the focused group's or session's colour through the palette.
func (m Model) cycleColor() tea.Cmd {
	c := m.c
	if it := m.currentGroupItem(); m.focus == focusGroups {
		if it.kind != itemGroup {
			return func() tea.Msg { return actionMsg{err: fmt.Errorf("select a group to colour")} }
		}
		next := palette[(slices.Index(palette, it.group.Color)+1)%len(palette)]
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.UpsertGroup(ctx, it.group.Path, next)
			return actionMsg{"group " + it.group.Path + " → " + next, err}
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

func (m Model) groupItems() []groupItem {
	items := []groupItem{{kind: itemAll, count: len(m.visibleSessions(nil))}}
	parents := map[int64]bool{}
	for _, g := range m.groups {
		parents[g.ParentID] = true
	}
	// m.groups arrives depth-first, so a collapsed node hides the run of
	// deeper nodes that follows it.
	hideBelow := -1
	for _, g := range m.groups {
		if hideBelow >= 0 && g.Depth > hideBelow {
			continue
		}
		hideBelow = -1
		gi := groupItem{kind: itemGroup, group: g, hasChildren: parents[g.ID]}
		gi.count = len(m.visibleSessions(&gi))
		items = append(items, gi)
		if gi.hasChildren && m.collapsed[g.ID] {
			hideBelow = g.Depth
		}
	}
	ungrouped := groupItem{kind: itemUngrouped}
	ungrouped.count = len(m.visibleSessions(&ungrouped))
	return append(items, ungrouped)
}

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

func (m *Model) selectGroup(id int64) {
	for i, it := range m.groupItems() {
		if it.kind == itemGroup && it.group.ID == id {
			m.groupCur = i
			return
		}
	}
}

func (m Model) currentGroupItem() groupItem {
	items := m.groupItems()
	if m.groupCur < 0 || m.groupCur >= len(items) {
		return items[0]
	}
	return items[m.groupCur]
}

// visibleSessions applies the group filter (nil = all) and the ended toggle.
// Order is by start time, newest first, so rows don't jump as sessions work.
func (m Model) visibleSessions(it *groupItem) []*model.Session {
	var out []*model.Session
	var inTree map[int64]bool
	if it != nil && it.kind == itemGroup {
		inTree = m.subtree(it.group.ID)
	}
	for i := range m.sessions {
		s := &m.sessions[i]
		if m.hideEnded && s.Status == model.StatusEnded {
			continue
		}
		if it != nil {
			switch it.kind {
			case itemGroup:
				if !slices.ContainsFunc(s.Groups, func(g model.Group) bool { return inTree[g.ID] }) {
					continue
				}
			case itemUngrouped:
				if len(s.Groups) > 0 {
					continue
				}
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

func (m *Model) rebuildRows() {
	it := m.currentGroupItem()
	m.rows = m.rows[:0]
	for _, s := range m.visibleSessions(&it) {
		m.rows = append(m.rows, treeRow{s: s})
		if m.expanded[s.ID] {
			for j := range s.Subagents {
				m.rows = append(m.rows, treeRow{s: s, a: &s.Subagents[j]})
			}
		}
	}
	m.sessCur = 0
	for i, r := range m.rows {
		if rowTarget(r) == m.selectedID {
			m.sessCur = i
			break
		}
	}
	m.sessCur = min(m.sessCur, max(0, len(m.rows)-1))
	if r := m.currentRow(); r != nil {
		m.selectedID = rowTarget(*r)
	}
}

func rowTarget(r treeRow) target {
	t := target{sessionID: r.s.ID}
	if r.a != nil {
		t.agentID = r.a.ID
	}
	return t
}

func (m *Model) moveSession(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.sessCur = max(0, min(len(m.rows)-1, m.sessCur+d))
	m.selectedID = rowTarget(m.rows[m.sessCur])
}

func (m Model) currentRow() *treeRow {
	if m.sessCur >= 0 && m.sessCur < len(m.rows) {
		return &m.rows[m.sessCur]
	}
	return nil
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
	case l.groups.contains(ev.X, ev.Y):
		m.focus = focusGroups
		if wheel != 0 {
			m.groupCur = max(0, min(len(m.groupItems())-1, m.groupCur+wheel))
		} else if i := ev.Y - l.groups.y - 1 + l.groupOff; i >= 0 && i < len(m.groupItems()) {
			m.groupCur = i
		}
		m.rebuildRows()
	case l.sessions.contains(ev.X, ev.Y):
		m.focus = focusSessions
		if wheel != 0 {
			m.moveSession(wheel)
			return m, nil
		}
		if i := ev.Y - l.sessions.y - 1 + l.sessOff; i >= 0 && i < len(m.rows) {
			again := i == m.sessCur
			m.sessCur = i
			m.selectedID = rowTarget(m.rows[i])
			if again { // click a selected row to open it
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
