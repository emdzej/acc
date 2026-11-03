package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/codex"
)

// Codex installs ACC's hooks into Codex's hooks.json. Codex runs user hooks
// only once they are reviewed and trusted (/hooks in Codex), by a hash of
// each hook, so the command line stays the same across reinstalls.
type Codex struct{}

const codexMarker = " hook codex"

// codexEvents are the Codex hook events ACC subscribes to. With no
// matcher, tool hooks fire for every tool. SessionEnd and Interrupt hooks
// may take at most 3 seconds.
var codexEvents = []hookEvent{
	{"SessionStart", false, 0},
	{"UserPromptSubmit", false, 0},
	{"PreToolUse", false, 0},
	{"PermissionRequest", false, 0},
	{"PostToolUse", false, 0},
	{"Stop", false, 0},
	{"Interrupt", false, 3},
	{"SubagentStart", false, 0},
	{"SubagentStop", false, 0},
	{"PreCompact", false, 0},
	{"SessionEnd", false, 3},
}

var codexHooks = hookFile{Events: codexEvents, Marker: codexMarker, Timeout: 5}

func (*Codex) Name() string { return codex.Agent }

func (*Codex) Detect() (Installation, bool) {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	return Installation{Binary: bin, Version: strings.TrimSpace(string(out))}, true
}

// hooksPath: user scope uses $CODEX_HOME/hooks.json, project scope the
// repo's .codex/hooks.json (loaded once the project is trusted).
func (*Codex) hooksPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".codex", "hooks.json"), nil
	}
	return filepath.Join(codex.Home(), "hooks.json"), nil
}

func (c *Codex) Plan(opts Options) ([]Change, error)          { return c.plan(opts, true) }
func (c *Codex) PlanUninstall(opts Options) ([]Change, error) { return c.plan(opts, false) }

func (c *Codex) plan(opts Options, install bool) ([]Change, error) {
	path, err := c.hooksPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	} else if err != nil {
		return nil, err
	}
	doc, err := parseOrdered(old)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := codexHooks.merge(&doc, opts.Bin, install); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// A file left empty (it held only ACC's hooks) is removed: next stays nil.
	var next []byte
	if len(doc) > 0 {
		if next, err = doc.marshalIndent(); err != nil {
			return nil, err
		}
	}
	if old != nil && next != nil && sameJSON(old, next) {
		next = old
	}
	if old == nil && next == nil {
		return nil, nil
	}
	note := "hooks: " + strings.Join(codexHooks.names(), ", ") + " (trust them once with /hooks in Codex)"
	if !install {
		note = "remove ACC hooks"
	}
	return []Change{{Path: path, Old: old, New: next, Note: note}}, nil
}
