package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/copilot"
)

// Copilot installs ACC's hooks into Copilot CLI as one file of its own,
// hooks/acc.json, which Copilot loads with every other file there.
type Copilot struct{}

const copilotMarker = " hook copilot"

// copilotEvents are the hooks ACC registers. PascalCase names get
// snake_case payloads. PermissionRequest is left out: Copilot sends it
// before every tool call, approved or not; a shown approval dialog comes
// as a permission_prompt notification.
var copilotEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
	"Notification", "Stop", "SubagentStop", "PreCompact", "ErrorOccurred", "SessionEnd"}

type copilotHooks struct {
	Version int                         `json:"version"`
	Hooks   map[string][]copilotCommand `json:"hooks"`
}

type copilotCommand struct {
	Type       string `json:"type"`
	Bash       string `json:"bash"`
	TimeoutSec int    `json:"timeoutSec"`
}

func (*Copilot) Name() string { return copilot.Agent }

func (*Copilot) Detect() (Installation, bool) {
	bin, err := exec.LookPath("copilot")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return Installation{Binary: bin, Version: v}, true
}

// hooksPath: user scope uses $COPILOT_HOME/hooks, project scope the repo's
// .github/hooks (which Copilot loads in trusted workspaces).
func (*Copilot) hooksPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".github", "hooks", "acc.json"), nil
	}
	return filepath.Join(copilot.Home(), "hooks", "acc.json"), nil
}

// readCopilotOwned reads an existing acc.json, refusing one with hooks ACC
// didn't write.
func readCopilotOwned(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc copilotHooks
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s exists and isn't ACC's hooks file; move it aside and re-run", path)
	}
	for _, hs := range doc.Hooks {
		for _, h := range hs {
			if !strings.HasSuffix(h.Bash, copilotMarker) {
				return nil, fmt.Errorf("%s has hooks ACC didn't write; move it aside and re-run", path)
			}
		}
	}
	return b, nil
}

func (c *Copilot) Plan(opts Options) ([]Change, error) {
	path, err := c.hooksPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readCopilotOwned(path)
	if err != nil {
		return nil, err
	}
	doc := copilotHooks{Version: 1, Hooks: map[string][]copilotCommand{}}
	for _, e := range copilotEvents {
		doc.Hooks[e] = []copilotCommand{{Type: "command", Bash: shellQuote(opts.Bin) + copilotMarker, TimeoutSec: 5}}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if old != nil && sameJSON(old, b) {
		b = old
	}
	return []Change{{Path: path, Old: old, New: b, Note: "hooks: " + strings.Join(copilotEvents, ", ")}}, nil
}

func (c *Copilot) PlanUninstall(opts Options) ([]Change, error) {
	path, err := c.hooksPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readCopilotOwned(path)
	if err != nil || old == nil {
		return nil, err
	}
	return []Change{{Path: path, Old: old, New: nil, Note: "remove ACC hooks"}}, nil
}
