package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexExisting = `{
  "hooks": {
    "PreToolUse": [
      { "matcher": "^Bash$", "hooks": [{ "type": "command", "command": "python3 policy.py", "timeout": 30, "statusMessage": "Checking" }] }
    ]
  }
}
`

func TestCodexInstallUninstall(t *testing.T) {
	c := &Codex{}
	dir := t.TempDir()
	opts := Options{Scope: ScopeProject, ProjectDir: dir, Bin: "/usr/local/bin/acc"}
	path := filepath.Join(dir, ".codex", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(codexExisting), 0o644)

	planApply(t, c.Plan, opts)
	b, _ := os.ReadFile(path)
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"statusMessage": "Checking"`) || len(doc.Hooks["PreToolUse"]) != 2 {
		t.Fatalf("the user's hook must survive:\n%s", b)
	}
	for _, e := range codexEvents {
		gs := doc.Hooks[e.Name]
		g := gs[len(gs)-1]
		if g.Matcher != "" || len(g.Hooks) != 1 || g.Hooks[0].Command != "/usr/local/bin/acc hook codex" {
			t.Errorf("%s: %+v", e.Name, g)
		}
		// Codex allows at most 3 seconds for these.
		if (e.Name == "SessionEnd" || e.Name == "Interrupt") && g.Hooks[0].Timeout != 3 {
			t.Errorf("%s timeout %d", e.Name, g.Hooks[0].Timeout)
		}
	}
	// Trust is per hook hash: a reinstall must not change a byte.
	if ch, _ := c.Plan(opts); len(ch) != 1 || !ch[0].Noop() {
		t.Error("second install not a no-op")
	}

	planApply(t, c.PlanUninstall, opts)
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "hook codex") || !strings.Contains(string(b), "policy.py") {
		t.Errorf("uninstall: %s", b)
	}

	// A hooks.json holding only ACC's hooks goes away entirely.
	os.Remove(path)
	planApply(t, c.Plan, opts)
	planApply(t, c.PlanUninstall, opts)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("ACC-only hooks.json not removed")
	}
	if ch, _ := c.PlanUninstall(opts); len(ch) != 0 {
		t.Error("uninstall of nothing should plan nothing")
	}
}
