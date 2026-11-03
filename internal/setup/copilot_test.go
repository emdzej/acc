package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopilotInstallUninstall(t *testing.T) {
	c := &Copilot{}
	dir := t.TempDir()
	opts := Options{Scope: ScopeProject, ProjectDir: dir, Bin: "/usr/local/bin/acc"}
	path := filepath.Join(dir, ".github", "hooks", "acc.json")

	planApply(t, c.Plan, opts)
	b, _ := os.ReadFile(path)
	var doc copilotHooks
	if err := json.Unmarshal(b, &doc); err != nil || doc.Version != 1 {
		t.Fatalf("%s: %v", b, err)
	}
	for _, e := range copilotEvents {
		if hs := doc.Hooks[e]; len(hs) != 1 || hs[0].Bash != "/usr/local/bin/acc hook copilot" || hs[0].Type != "command" {
			t.Errorf("%s: %+v", e, hs)
		}
	}
	if _, ok := doc.Hooks["PermissionRequest"]; ok {
		t.Error("PermissionRequest fires for auto-approved calls too; ACC mustn't subscribe")
	}
	if ch, _ := c.Plan(opts); len(ch) != 1 || !ch[0].Noop() {
		t.Error("second install not a no-op")
	}
	planApply(t, c.PlanUninstall, opts)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("uninstall leaves the file")
	}

	// A file of the same name with someone else's hooks is left alone.
	os.WriteFile(path, []byte(`{"version":1,"hooks":{"Stop":[{"type":"command","bash":"./notify.sh"}]}}`), 0o644)
	if _, err := c.Plan(opts); err == nil || !strings.Contains(err.Error(), "didn't write") {
		t.Errorf("foreign hooks file: %v", err)
	}
}

const geminiExisting = `{
  "general": {"vimMode": true},
  "hooks": {
    "BeforeTool": [
      {"matcher": "write_file", "hooks": [{"name": "lint", "type": "command", "command": "./lint.sh", "timeout": 30000}]}
    ]
  },
  "security": {"auth": {"selectedType": "oauth-personal"}}
}
`

func TestGeminiInstallUninstall(t *testing.T) {
	g := &Gemini{}
	dir := t.TempDir()
	opts := Options{Scope: ScopeProject, ProjectDir: dir, Bin: "/usr/local/bin/acc"}
	path := filepath.Join(dir, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(geminiExisting), 0o644)

	planApply(t, g.Plan, opts)
	b, _ := os.ReadFile(path)
	var doc struct {
		General map[string]any `json:"general"`
		Hooks   map[string][]struct {
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
	if doc.General["vimMode"] != true || len(doc.Hooks["BeforeTool"]) != 2 || !strings.Contains(string(b), `"name": "lint"`) {
		t.Fatalf("the user's settings must survive:\n%s", b)
	}
	for _, e := range geminiEvents {
		gs := doc.Hooks[e.Name]
		h := gs[len(gs)-1]
		if h.Matcher != "" || len(h.Hooks) != 1 || h.Hooks[0].Command != "/usr/local/bin/acc hook gemini" || h.Hooks[0].Timeout != 5000 {
			t.Errorf("%s: %+v", e.Name, h)
		}
	}
	if ch, _ := g.Plan(opts); len(ch) != 1 || !ch[0].Noop() {
		t.Error("second install not a no-op")
	}
	planApply(t, g.PlanUninstall, opts)
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "hook gemini") || !strings.Contains(string(b), "lint.sh") || !strings.Contains(string(b), "oauth-personal") {
		t.Errorf("uninstall: %s", b)
	}
}
