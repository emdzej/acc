package tui

import (
	"os"
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
	m.selectedID = target{sessionID: "7116efd3-730d"}
	m.rebuildRows()
	m.openCurrent()
	m.latest = transcript.Latest{Event: "PostToolUse", Kind: "tool", Tool: "Bash", Input: "go test ./...", Output: "ok  \tpkg\t0.3s", TS: time.Now().UnixMilli()}
	m.messages = []model.Message{{ID: 1, Body: "please run the tests", CreatedAt: time.Now().UnixMilli()}}
	out := render(t, m, 140, 30)
	for _, want := range []string{"api-server · claude-code", "⚙ Bash", "go test ./...", "Messages", "please run the tests", "└ ● Explore", "platform/api", "⚠ api-server", "⚠ needs permission", "Bash: rm -rf build", "⚑ 1 need you", "$0.42", "cache 2.4M read", "+12 -3", "Σ $0.42", "5h 80%", "7d 42%", "context 82% of 200.0k", "ctx 82%", "model Opus 5.5"} {
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
	m.selectedID = target{sessionID: "7116efd3-730d"}
	m.rebuildRows()
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

func TestGroupTreeFilterAndCollapse(t *testing.T) {
	m := fixture()
	m.groupCur = 1 // platform: includes platform/api
	m.rebuildRows()
	if len(m.rows) != 1 || m.rows[0].s.Title != "api-server" {
		t.Fatalf("platform filter should include descendant groups, got %d rows", len(m.rows))
	}
	m.collapsed[1] = true
	if n := len(m.groupItems()); n != 4 { // All, platform, web, ungrouped
		t.Fatalf("collapsed platform should hide api: %d items", n)
	}
}

// TestDocsScreen writes a true-colour render for the docs site when
// ACC_DOCS_SCREEN is set: ACC_DOCS_SCREEN=/tmp/screen.ansi go test ./internal/tui -run DocsScreen
func TestDocsScreen(t *testing.T) {
	out := os.Getenv("ACC_DOCS_SCREEN")
	if out == "" {
		t.Skip("set ACC_DOCS_SCREEN to write the docs screenshot")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := fixture()
	m.expanded["7116efd3-730d"] = true
	m.selectedID = target{sessionID: "7116efd3-730d"}
	m.rebuildRows()
	m.openCurrent()
	m.latest = transcript.Latest{Event: "PostToolUse", Kind: "tool", Tool: "Bash", Input: "go test ./...",
		Output: "ok  \tgithub.com/acme/api/internal/store\t0.31s\nok  \tgithub.com/acme/api/internal/http\t0.12s", TS: time.Now().UnixMilli()}
	m.messages = []model.Message{{ID: 1, Body: "please run the tests before committing", CreatedAt: time.Now().UnixMilli(), DeliveredAt: time.Now().UnixMilli()}}
	m.focus = focusSessions
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	if err := os.WriteFile(out, []byte(mm.(Model).View()), 0o644); err != nil {
		t.Fatal(err)
	}
}
