package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emdzej/acc/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "acc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEventLifecycleAndDelivery(t *testing.T) {
	s, ctx := open(t), context.Background()
	ev := func(name string, deliver bool) []model.Message {
		t.Helper()
		msgs, err := s.RecordEvent(ctx, model.Event{Agent: "claude-code", SessionID: "abc123", Name: name, Cwd: "/w/proj", Deliver: deliver})
		if err != nil {
			t.Fatal(err)
		}
		return msgs
	}
	ev("SessionStart", true)
	if _, err := s.Enqueue(ctx, model.SendRequest{SessionID: "abc", Body: "hi"}, "test"); err != nil {
		t.Fatal(err)
	}
	if got := ev("Notification", false); len(got) != 0 {
		t.Fatal("non-delivering event claimed messages")
	}
	got := ev("PostToolUse", true)
	if len(got) != 1 || got[0].Body != "hi" {
		t.Fatalf("want 1 message, got %+v", got)
	}
	if again := ev("Stop", true); len(again) != 0 {
		t.Fatal("message delivered twice")
	}

	ss, _ := s.ListSessions(ctx)
	if len(ss) != 1 || ss[0].Status != model.StatusIdle || ss[0].Title != "proj" || ss[0].PendingCount != 0 {
		t.Fatalf("unexpected session: %+v", ss)
	}
}

func TestGroupsAndGroupSend(t *testing.T) {
	s, ctx := open(t), context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		s.RecordEvent(ctx, model.Event{SessionID: id, Name: "SessionStart"})
	}
	s.RecordEvent(ctx, model.Event{SessionID: "s3", Name: "SessionEnd"})
	if _, err := s.UpsertGroup(ctx, "api", "#FF0000"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertGroup(ctx, "bad", "red"); err == nil {
		t.Fatal("accepted invalid colour")
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if err := s.Assign(ctx, id, "api", false); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := s.Enqueue(ctx, model.SendRequest{Group: "api", Body: "rebase"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("group send should skip ended session, got %d", len(msgs))
	}
	if _, err := s.ResolveSession(ctx, "s"); err == nil {
		t.Fatal("ambiguous prefix resolved")
	}
	if err := s.DeleteGroup(ctx, "api"); err != nil {
		t.Fatal(err)
	}
	ss, _ := s.ListSessions(ctx)
	for _, x := range ss {
		if len(x.Groups) != 0 {
			t.Fatal("group membership not cascaded on delete")
		}
	}
}

func TestSubagentsAreChildrenAndNeverClaimMessages(t *testing.T) {
	s, ctx := open(t), context.Background()
	rec := func(ev model.Event) []model.Message {
		t.Helper()
		ev.SessionID = "main"
		msgs, err := s.RecordEvent(ctx, ev)
		if err != nil {
			t.Fatal(err)
		}
		return msgs
	}
	rec(model.Event{Name: "UserPromptSubmit", Deliver: true})
	rec(model.Event{Name: "PostToolUse", ToolName: "Edit", Deliver: true})
	s.Enqueue(ctx, model.SendRequest{SessionID: "main", Body: "for main"}, "test")

	rec(model.Event{Name: "SubagentStart", AgentID: "a1", AgentType: "Explore"})
	if got := rec(model.Event{Name: "PostToolUse", ToolName: "Bash", Summary: "ls", AgentID: "a1", Deliver: true}); len(got) != 0 {
		t.Fatal("subagent claimed a main-session message")
	}
	rec(model.Event{Name: "SubagentStop", AgentID: "a1", Message: "found 3 files"})

	ss, _ := s.ListSessions(ctx)
	if len(ss) != 1 {
		t.Fatalf("subagent created its own session: %+v", ss)
	}
	m := ss[0]
	if m.LastTool != "Edit" || m.PendingCount != 1 {
		t.Errorf("parent state overwritten by subagent: tool=%q pending=%d", m.LastTool, m.PendingCount)
	}
	if len(m.Subagents) != 1 {
		t.Fatalf("want 1 subagent, got %+v", m.Subagents)
	}
	a := m.Subagents[0]
	if a.Type != "Explore" || a.Status != model.SubagentDone || a.LastTool != "Bash" || a.Result != "found 3 files" {
		t.Errorf("unexpected subagent: %+v", a)
	}
	if got := rec(model.Event{Name: "Stop", Deliver: true}); len(got) != 1 {
		t.Fatal("main agent did not receive its message")
	}
}

func TestGroupTree(t *testing.T) {
	s, ctx := open(t), context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		s.RecordEvent(ctx, model.Event{SessionID: id, Name: "SessionStart"})
	}
	if _, err := s.UpsertGroup(ctx, "platform", "#112233"); err != nil {
		t.Fatal(err)
	}
	// Missing ancestors are created; the leaf inherits when no colour is given.
	api, err := s.UpsertGroup(ctx, " platform / backend/api ", "")
	if err != nil {
		t.Fatal(err)
	}
	if api.Path != "platform/backend/api" || api.Depth != 2 || api.Display != "#112233" {
		t.Fatalf("api = %+v", api)
	}
	if _, err := s.UpsertGroup(ctx, "web/api", "#aabbcc"); err != nil {
		t.Fatal("same name under another parent should be allowed:", err)
	}
	s.Assign(ctx, "s1", "platform/backend/api", false)
	s.Assign(ctx, "s2", "platform", false)
	s.Assign(ctx, "s3", "web/api", false)

	msgs, err := s.Enqueue(ctx, model.SendRequest{Group: "platform", Body: "x"}, "test")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("group send should include descendants: %d %v", len(msgs), err)
	}

	ss, _ := s.ListSessions(ctx)
	for _, x := range ss {
		if x.ID == "s1" && x.DisplayColor() != "#112233" {
			t.Errorf("s1 should inherit platform colour, got %q", x.DisplayColor())
		}
	}
	s.SetSessionColor(ctx, "s1", "#ff0000")
	ss, _ = s.ListSessions(ctx)
	for _, x := range ss {
		if x.ID == "s1" && x.DisplayColor() != "#ff0000" {
			t.Errorf("own session colour should win, got %q", x.DisplayColor())
		}
	}

	if err := s.DeleteGroup(ctx, "platform"); err != nil {
		t.Fatal(err)
	}
	gs, _ := s.ListGroups(ctx)
	var paths []string
	for _, g := range gs {
		paths = append(paths, g.Path)
	}
	if strings.Join(paths, ",") != "web,web/api" {
		t.Fatalf("subtree not deleted: %v", paths)
	}
}

func TestUnknownNotificationNeedsAttention(t *testing.T) {
	s, ctx := open(t), context.Background()
	s.RecordEvent(ctx, model.Event{SessionID: "n", Name: "SessionStart"})
	s.RecordEvent(ctx, model.Event{SessionID: "n", Name: "Notification", NotificationType: "something_new", Message: "look"})
	ss, _ := s.ListSessions(ctx)
	if ss[0].Attention != model.AttentionNotice || ss[0].AttentionDetail != "look" {
		t.Fatalf("attention = %q %q", ss[0].Attention, ss[0].AttentionDetail)
	}
	s.RecordEvent(ctx, model.Event{SessionID: "n", Name: "UserPromptSubmit"})
	ss, _ = s.ListSessions(ctx)
	if ss[0].Attention != "" {
		t.Fatal("prompt should clear attention")
	}
}
