package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodePluginInstallUninstall(t *testing.T) {
	o := &OpenCode{}
	dir := t.TempDir()
	opts := Options{Scope: ScopeProject, ProjectDir: dir}
	path := filepath.Join(dir, ".opencode", "plugins", "acc.js")

	planApply(t, o.Plan, opts)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, openCodeMarker) || strings.Contains(s, "__ACC_HOME__") || strings.Contains(s, "__ACC_SERVER__") {
		t.Fatalf("placeholders not filled or marker missing:\n%.300s", s)
	}
	if ch, _ := o.Plan(opts); !ch[0].Noop() {
		t.Error("second install not a no-op")
	}
	planApply(t, o.PlanUninstall, opts)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("plugin not removed")
	}
	if ch, _ := o.PlanUninstall(opts); len(ch) != 0 {
		t.Error("uninstall of nothing should plan nothing")
	}

	// Someone else's acc.js is never overwritten.
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("export default {}"), 0o644)
	if _, err := o.Plan(opts); err == nil {
		t.Error("overwrote a foreign plugin file")
	}
}
