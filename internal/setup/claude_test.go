package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const existing = `{
  "theme": "dark",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "lint.sh && echo <ok>", "timeout": 3, "statusMessage": "linting" }]
      }
    ]
  },
  "env": { "A": "1" }
}
`

func projectOpts(t *testing.T) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.local.json")
	return Options{Scope: ScopeProject, ProjectDir: dir, Bin: "/usr/local/bin/acc"}, path
}

func planApply(t *testing.T, plan func(Options) ([]Change, error), opts Options) []Change {
	t.Helper()
	ch, err := plan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ch); err != nil {
		t.Fatal(err)
	}
	return ch
}

func TestClaudeInstallMergesAndIsIdempotent(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(existing), 0o600)

	planApply(t, c.Plan, opts)
	got, _ := os.ReadFile(path)
	s := string(got)

	// User config survives, in order, with readable commands and unknown fields.
	for _, want := range []string{`"theme": "dark"`, `lint.sh && echo <ok>`, `"statusMessage": "linting"`, `"A": "1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	if strings.Index(s, `"theme"`) > strings.Index(s, `"env"`) {
		t.Error("top-level key order changed")
	}

	var parsed struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, e := range claudeEvents {
		n := 0
		for _, g := range parsed.Hooks[e.Name] {
			for _, h := range g.Hooks {
				if strings.HasSuffix(h.Command, hookMarker) {
					n++
					if e.Matcher && g.Matcher != "*" {
						t.Errorf("%s: matcher %q", e.Name, g.Matcher)
					}
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: %d ACC hooks, want 1", e.Name, n)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}

	// Second run is a no-op.
	ch, err := c.Plan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !ch[0].Noop() {
		t.Errorf("second plan not a no-op:\n%s", Diff(ch[0]))
	}
}

func TestClaudeUninstallRestoresUserHooks(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(existing), 0o600)

	planApply(t, c.Plan, opts)
	planApply(t, c.PlanUninstall, opts)
	got, _ := os.ReadFile(path)
	if !sameJSON([]byte(existing), got) {
		t.Errorf("uninstall did not restore original:\n%s", got)
	}
	backups, _ := filepath.Glob(path + ".acc-bak-*")
	if len(backups) == 0 {
		t.Error("no backup written")
	}
}

func TestClaudeFreshFile(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	planApply(t, c.Plan, opts)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	planApply(t, c.PlanUninstall, opts)
	got, _ := os.ReadFile(path)
	if strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("want empty object after uninstall, got %s", got)
	}
}

func TestBinWithSpacesIsQuoted(t *testing.T) {
	if got := shellQuote("/Users/me/My Tools/acc"); got != `'/Users/me/My Tools/acc'` {
		t.Errorf("got %s", got)
	}
}

func readJSON(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func statusCmd(t *testing.T, path string) string {
	t.Helper()
	var sl struct{ Command string }
	json.Unmarshal(readJSON(t, path)["statusLine"], &sl)
	return sl.Command
}

func TestStatusLineFreshInstallAndUninstall(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	planApply(t, c.Plan, opts)
	if got := statusCmd(t, path); got != "/usr/local/bin/acc statusline" {
		t.Fatalf("statusLine = %q", got)
	}
	planApply(t, c.PlanUninstall, opts)
	if _, ok := readJSON(t, path)["statusLine"]; ok {
		t.Fatal("statusLine not removed")
	}
}

func TestStatusLineWrapsUserCommand(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	userCmd := `bash -c 'echo it'\''s me'` // quoting that must survive the round trip
	ub, _ := json.Marshal(map[string]any{"statusLine": map[string]any{"type": "command", "command": userCmd, "padding": 0}})
	user := string(ub)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(user), 0o600)

	planApply(t, c.Plan, opts)
	cmd := statusCmd(t, path)
	chain, ours := parseACCStatusLine(cmd)
	if !ours || chain != userCmd {
		t.Fatalf("wrapped %q → chain %q ours=%v", cmd, chain, ours)
	}
	if !strings.Contains(string(readJSON(t, path)["statusLine"]), `"padding"`) {
		t.Error("extra statusLine fields lost")
	}

	// Re-running (e.g. after the binary moved) keeps the original chained once.
	opts.Bin = "/opt/acc"
	planApply(t, c.Plan, opts)
	if chain2, _ := parseACCStatusLine(statusCmd(t, path)); chain2 != chain {
		t.Fatalf("re-install changed chain: %q", chain2)
	}
	ch, _ := c.Plan(opts)
	if !ch[0].Noop() {
		t.Error("install not idempotent")
	}

	planApply(t, c.PlanUninstall, opts)
	if !sameJSON([]byte(user), mustRead(t, path)) {
		t.Fatalf("uninstall did not restore user status line:\n%s", mustRead(t, path))
	}
}

func TestNoStatusLineLeavesUserCommand(t *testing.T) {
	c := &Claude{}
	opts, path := projectOpts(t)
	opts.NoStatusLine = true
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"my-line"}}`), 0o600)
	planApply(t, c.Plan, opts)
	if got := statusCmd(t, path); got != "my-line" {
		t.Fatalf("user statusLine changed: %q", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
