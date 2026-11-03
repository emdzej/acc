package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/transcript"
)

func fixture() Model {
	os.Setenv("ACC_HOME", os.TempDir()+"/acc-tui-test") // keep real ~/.acc/tui.json out of tests
	os.Remove(os.TempDir() + "/acc-tui-test/tui.json")
	now := time.Now().UnixMilli()
	platform := model.Group{ID: 1, Name: "platform", Path: "platform", Color: "#61afef", Display: "#61afef"}
	api := model.Group{ID: 2, ParentID: 1, Name: "api", Path: "platform/api", Depth: 1, Display: "#61afef"}
	web := model.Group{ID: 3, Name: "web", Path: "web", Color: "#c678dd", Display: "#c678dd"}
	m := New(&client.Client{BaseURL: "http://127.0.0.1:7317"}, Options{Embedded: true})
	m.groups = []model.Group{platform, api, web}
	m.sessions = []model.Session{
		{ID: "7116efd3-730d", Agent: "claude-code", Title: "api-server", Cwd: "/Users/me/code/api", Status: "working",
			StartedAt: now - 3000, LastSeen: now - 2000, Groups: []model.Group{api}, PendingCount: 1,
			Attention: "permission", AttentionDetail: "Bash: rm -rf build", AttentionSince: now - 5000,
			Model: "Opus 5.5", ContextPct: ptr(82.0), ContextSize: 200000,
			Goal:      &model.Goal{Objective: "all tests pass", Status: model.GoalActive, Reason: "2 tests still fail", Rounds: 2},
			Usage:     &model.Usage{CostUSD: 0.4213, CostKnown: true, InputTokens: 1200, OutputTokens: 5300, CacheReadTokens: 2_400_000, LinesAdded: 12, LinesRemoved: 3, Models: []string{"claude-opus-5-5"}},
			Subagents: []model.Subagent{{ID: "ad85", SessionID: "7116efd3-730d", Type: "Explore", Status: "running", LastTool: "Grep", StartedAt: now - 1000, LastSeen: now}}},
		{ID: "9b2da2a2-c451", Agent: "claude-code", Title: "web-ui", Status: "waiting", Color: "#e06c75",
			StartedAt: now - 2000, LastSeen: now - 9000, Groups: []model.Group{web}},
		{ID: "b667a6fd-0000", Agent: "claude-code", Title: "scratch", Status: "ended", StartedAt: now - 1000, LastSeen: now - 60000},
	}
	m.plan = []model.PlanUsage{{Agent: "claude-code", UpdatedAt: now, Windows: []model.PlanWindow{
		{Key: "five_hour", Label: "5h", UsedPct: 80, ResetsAt: now + 2*3600*1000}, {Key: "seven_day", Label: "7d", UsedPct: 42}}}}
	m.rebuildRows()
	return m
}

func ptr[T any](v T) *T { return &v }

func render(t *testing.T, m Model, w, h int) string {
	t.Helper()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	out := mm.(Model).View()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d: rendered %d lines", w, h, len(lines))
	}
	for i, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("%dx%d: line %d is %d cells wide", w, h, i, lw)
		}
	}
	if os.Getenv("TUI_DUMP") != "" {
		t.Logf("\n%s", ansi.Strip(out))
	}
	return ansi.Strip(out)
}

func TestLayoutFitsTerminal(t *testing.T) {
	for _, sz := range [][2]int{{140, 30}, {100, 24}, {80, 24}, {60, 15}} {
		render(t, fixture(), sz[0], sz[1])
	}
}

func TestOpenSessionShowsLatestThenTranscript(t *testing.T) {
	m := fixture()
	m.expanded["7116efd3-730d"] = true
	m.rebuildRows()
	selectSession(&m, "7116efd3-730d")
	m.openCurrent()
	m.latest = transcript.Latest{Event: "PostToolUse", Kind: "tool", Tool: "Bash", Input: "go test ./...", Output: "ok  \tpkg\t0.3s", TS: time.Now().UnixMilli()}
	m.messages = []model.Message{{ID: 1, Body: "please run the tests", CreatedAt: time.Now().UnixMilli()}}
	out := render(t, m, 140, 30)
	for _, want := range []string{"api-server · claude-code", "⚙ Bash", "go test ./...", "Messages", "please run the tests", "└ ● Explore", "platform/api", "⚠ api-server", "⚠ needs permission", "Bash: rm -rf build", "⚑ 1 need you", "$0.42", "cache 2.4M read", "+12 -3", "Σ $0.42", "5h 80%", "7d 42%", "context 82% of 200.0k", "ctx 82%", "model Opus 5.5", "◎ active  all tests pass", "round 2", "2 tests still fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}

	m.toggleTracking()
	m.entries = []transcript.Entry{
		{Kind: transcript.KindPrompt, Role: "user", Text: "fix the flaky test"},
		{Kind: transcript.KindToolUse, Tool: "Bash", Text: "go test ./..."},
		{Kind: transcript.KindToolResult, Text: "FAIL", Error: true},
	}
	m.tOffset = 100
	out = render(t, m, 140, 30)
	for _, want := range []string{"Transcript · live", "you fix the flaky test", "⚙   Bash  go test"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestCostToggle(t *testing.T) {
	t.Setenv("ACC_HOME", t.TempDir())
	m := fixture()
	m.rebuildRows()
	selectSession(&m, "7116efd3-730d")
	m.openCurrent()
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$")})
	m = mm.(Model)
	out := render(t, m, 140, 30)
	if strings.Contains(out, "$0.42") {
		t.Error("cost still shown after toggling off")
	}
	if !strings.Contains(out, "cache 2.4M read") {
		t.Error("tokens should stay visible when cost is hidden")
	}
	if New(m.c, Options{}).showCost != m.showCost {
		t.Error("toggle not persisted")
	}
}

func selectSession(m *Model, id string) {
	for i, r := range m.rows {
		if r.s != nil && r.a == nil && r.s.ID == id {
			m.cur, m.selKey = i, r.key()
			return
		}
	}
}

func TestSingleTree(t *testing.T) {
	m := fixture()
	var got []string
	for _, r := range m.rows {
		switch {
		case r.isGroup():
			got = append(got, fmt.Sprintf("%d:[%s %d]", r.depth, r.g.Name, r.count))
		default:
			got = append(got, fmt.Sprintf("%d:%s", r.depth, r.s.Title))
		}
	}
	want := "0:[platform 1] 1:[api 1] 2:api-server 0:[web 1] 1:web-ui 0:scratch"
	if strings.Join(got, " ") != want {
		t.Fatalf("tree\n got %s\nwant %s", strings.Join(got, " "), want)
	}

	// ← on a session goes to its group; ← again folds it; → unfolds.
	selectSession(&m, "7116efd3-730d")
	m.selectParent()
	if g := m.selGroup(); g == nil || g.Name != "api" {
		t.Fatalf("parent of api-server should be api, got %+v", m.currentRow())
	}
	m.setExpanded(false)
	if len(m.rows) != 5 {
		t.Fatalf("folding api should hide its session: %d rows", len(m.rows))
	}
	if g := m.selGroup(); g == nil || g.Name != "api" {
		t.Fatal("selection should stay on the folded group")
	}
	m.setExpanded(true)
	if len(m.rows) != 6 {
		t.Fatalf("unfold: %d rows", len(m.rows))
	}

	// A session in two groups shows under both.
	m.sessions[1].Groups = append(m.sessions[1].Groups, m.groups[1])
	m.rebuildRows()
	n := 0
	for _, r := range m.rows {
		if r.s != nil && r.s.Title == "web-ui" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("web-ui in two groups should show twice, got %d", n)
	}

	// Hiding ended sessions drops them from the tree.
	m.hideEnded = true
	m.rebuildRows()
	for _, r := range m.rows {
		if r.s != nil && r.s.Status == model.StatusEnded {
			t.Fatal("ended session still listed")
		}
	}
}

// TestDocsScreen writes a true-colour render for the docs site when
// ACC_DOCS_SCREEN is set: ACC_DOCS_SCREEN=/tmp/screen.ansi go test ./internal/tui -run DocsScreen
// (ACC_DOCS_SIZE=140x34 for another size).
func TestDocsScreen(t *testing.T) {
	out := os.Getenv("ACC_DOCS_SCREEN")
	if out == "" {
		t.Skip("set ACC_DOCS_SCREEN to write the docs screenshot")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := fixture()
	m.expanded["7116efd3-730d"] = true
	m.rebuildRows()
	selectSession(&m, "7116efd3-730d")
	m.openCurrent()
	m.latest = transcript.Latest{Event: "PostToolUse", Kind: "tool", Tool: "Bash", Input: "go test ./...",
		Output: "ok  \tgithub.com/acme/api/internal/store\t0.31s\nok  \tgithub.com/acme/api/internal/http\t0.12s", TS: time.Now().UnixMilli()}
	m.messages = []model.Message{{ID: 1, Body: "please run the tests before committing", CreatedAt: time.Now().UnixMilli(), DeliveredAt: time.Now().UnixMilli()}}
	m.plan[0].Windows[0].LimitAt = time.Now().Add(70 * time.Minute).UnixMilli() // show the pace warning
	m.focus = focusSessions
	w, h := 92, 24
	fmt.Sscanf(os.Getenv("ACC_DOCS_SIZE"), "%dx%d", &w, &h) // e.g. 140x34 for a PNG
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if err := os.WriteFile(out, []byte(mm.(Model).View()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSuggester(t *testing.T) {
	s := newSuggester([]string{"platform", "platform/api", "web", "tools/apidocs"}, true)
	s.filter("api")
	if strings.Join(s.shown, ",") != "platform/api,tools/apidocs" { // segment start ranks before substring
		t.Fatalf("shown %v", s.shown)
	}
	if v, ok := s.resolve("api"); !ok || v != "platform/api" {
		t.Errorf("Enter should take the highlighted match, got %q %v", v, ok)
	}
	if v, ok := s.resolve("WEB"); !ok || v != "web" {
		t.Errorf("exact match is case-insensitive, got %q", v)
	}
	s.filter("nope")
	if _, ok := s.resolve("nope"); ok {
		t.Error("strict prompt accepted an unknown group")
	}
	free := newSuggester([]string{"/code/api"}, false)
	free.filter("/tmp/new")
	if v, ok := free.resolve("/tmp/new"); !ok || v != "/tmp/new" {
		t.Errorf("free prompt should keep typed text, got %q %v", v, ok)
	}
}

func TestAssignPromptOffersOnlyValidGroups(t *testing.T) {
	m := fixture()
	m.w, m.h = 120, 30
	selectSession(&m, "7116efd3-730d") // in platform/api
	press := func(s string) {
		for _, r := range s {
			mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			m = mm.(Model)
		}
	}
	press("g")
	if m.mode != inputAssign || m.sugg == nil || strings.Join(m.sugg.shown, ",") != "platform,web" {
		t.Fatalf("add should offer groups the session isn't in: %+v", m.sugg)
	}
	press("we")
	if got := ansi.Strip(m.viewKeys()); !strings.Contains(got, " web ") {
		t.Errorf("key bar should list matches: %q", got)
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(Model)
	if m.input.Value() != "web" {
		t.Errorf("Tab should complete, got %q", m.input.Value())
	}

	m = fixture()
	selectSession(&m, "7116efd3-730d")
	press("u")
	if m.sugg == nil || strings.Join(m.sugg.shown, ",") != "platform/api" {
		t.Fatalf("remove should offer only the session's groups: %+v", m.sugg)
	}
	press("zzz")
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.mode != inputUnassign || m.flashOK {
		t.Error("unknown group should keep the prompt open with an error")
	}
}

func TestJumpToSession(t *testing.T) {
	m := fixture()
	m.collapsed[3] = true // web folded: web-ui is hidden
	m.rebuildRows()
	mm, _ := m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mm.(Model)
	if m.mode != inputJump || m.sugg == nil {
		t.Fatalf("/ should open the jump prompt, mode=%v", m.mode)
	}
	for _, r := range "web-u" {
		mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	if got := m.sugg.selected(); !strings.HasPrefix(got, "web-ui · web") {
		t.Fatalf("highlighted %q", got)
	}
	mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.mode != inputNone || m.open == nil || m.open.sessionID != "9b2da2a2-c451" {
		t.Fatalf("open=%+v mode=%v", m.open, m.mode)
	}
	if r := m.currentRow(); r == nil || r.s == nil || r.s.ID != "9b2da2a2-c451" {
		t.Fatalf("cursor not on web-ui: %+v", r)
	}
	if m.collapsed[3] {
		t.Error("jumping should unfold the session's group")
	}

	// An unknown name keeps the prompt open.
	mm, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mm.(Model)
	for _, r := range "zzz" {
		mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if mm.(Model).mode != inputJump {
		t.Error("no match should keep the prompt open")
	}
}

func TestHistoryDialog(t *testing.T) {
	m := fixture()
	m.w, m.h = 120, 36
	mm, _ := m.Update(historyMsg{past: []model.PastSession{
		{Agent: "claude-code", ID: "aaaa1111-old", Cwd: "/w/legacy", Title: "port the legacy importer", LastActive: 1,
			TranscriptPath: "/c/projects/w-legacy/aaaa1111-old.jsonl", Size: 3 << 20},
		{Agent: "opencode", ID: "9b2da2a2-c451", Cwd: "/w/docs", Title: "docs", LastActive: 2, Running: true, Tracked: true},
	}})
	m = mm.(Model)
	if m.mode != inputHistory || m.hist == nil || len(m.hist.shown) != 2 {
		t.Fatalf("history dialog: mode=%v", m.mode)
	}
	for _, r := range "legacy imp" {
		mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	if len(m.hist.shown) != 1 || m.hist.selected().ID != "aaaa1111-old" {
		t.Fatalf("filtered to %+v", m.hist.shown)
	}
	v := m.View()
	for _, want := range []string{"History", "1 of 2", "/w/legacy", "aaaa1111-old", "3.0 MB", "not in ACC", "⏎ resume", "^A add to ACC only"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	mm, cmd := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if m = mm.(Model); m.mode != inputNone || m.hist != nil || cmd == nil || m.openWhenListed != "aaaa1111-old" {
		t.Fatalf("Enter should resume: mode=%v open=%q", m.mode, m.openWhenListed)
	}

	// A session live in ACC opens instead of resuming a second copy.
	mm, _ = m.Update(historyMsg{past: []model.PastSession{{Agent: "opencode", ID: "9b2da2a2-c451", Cwd: "/w/docs", Title: "docs", Running: true, Tracked: true}}})
	m = mm.(Model)
	if !strings.Contains(m.View(), "⏎ open") {
		t.Error("live session should offer open")
	}
	mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if m = mm.(Model); m.open == nil || m.open.sessionID != "9b2da2a2-c451" {
		t.Fatalf("Enter should open the live session, open=%+v", m.open)
	}

	mm, _ = m.Update(historyMsg{past: []model.PastSession{{Agent: "claude-code", ID: "x", Cwd: "/w"}}})
	mm, cmd = mm.(Model).updateInput(tea.KeyMsg{Type: tea.KeyEsc})
	if m = mm.(Model); m.mode != inputNone || m.hist != nil || cmd != nil {
		t.Error("Esc should close without acting")
	}
}

func TestForgetPrompt(t *testing.T) {
	m := fixture()
	m.focus = focusSessions
	for i, r := range m.rows {
		if r.s != nil && r.a == nil {
			m.cur = i
			break
		}
	}
	s := m.currentRow().s
	mm, _ := m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = mm.(Model)
	if m.mode != inputConfirmForget || !strings.Contains(m.input.Prompt, "forget "+s.Title) {
		t.Fatalf("x: mode=%v prompt=%q", m.mode, m.input.Prompt)
	}
	mm, cmd := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter}) // empty = no
	if mm.(Model).mode != inputNone {
		t.Fatal("Enter should close the prompt")
	}
	if msg, ok := cmd().(actionMsg); !ok || msg.ok != "cancelled" {
		t.Errorf("empty answer should cancel, got %+v", msg)
	}
}

func TestStepSession(t *testing.T) {
	m := fixture()
	var ids []string // sessions in tree order, each once
	seen := map[string]bool{}
	for _, r := range m.rows {
		if r.s != nil && r.a == nil && !seen[r.s.ID] {
			seen[r.s.ID] = true
			ids = append(ids, r.s.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatal("fixture needs two sessions")
	}
	m.jumpTo(ids[0])
	if m.focus != focusDetail {
		t.Fatal("opening should focus the session panel")
	}
	mm, _ := m.updateKeys(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = mm.(Model)
	if m.open == nil || m.open.sessionID != ids[1] || m.focus != focusDetail {
		t.Fatalf("shift+down: open=%+v focus=%v, want %s", m.open, m.focus, ids[1])
	}
	mm, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = mm.(Model)
	if m.open.sessionID != ids[0] {
		t.Fatalf("[: open=%s, want %s", m.open.sessionID, ids[0])
	}
	mm, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyShiftUp})
	if m = mm.(Model); m.open.sessionID != ids[0] || m.flash != "first session" {
		t.Errorf("at the top: open=%s flash=%q", m.open.sessionID, m.flash)
	}
}

func TestLaunchAgentStep(t *testing.T) {
	m := fixture()
	mm, _ := m.startLaunch()
	m = mm.(Model)
	if m.mode != inputLaunchAgent || m.sugg == nil || !slices.Contains(m.sugg.shown, "pi") || !slices.Contains(m.sugg.shown, "claude") {
		t.Fatalf("agent step: mode=%v sugg=%+v", m.mode, m.sugg)
	}
	for _, r := range "pi" {
		mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	mm, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if m = mm.(Model); m.mode != inputLaunchDir || m.launch.Agent != "pi" || !strings.Contains(m.input.Prompt, "(pi)") {
		t.Fatalf("after picking pi: mode=%v agent=%q prompt=%q", m.mode, m.launch.Agent, m.input.Prompt)
	}
}
