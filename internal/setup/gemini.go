package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/gemini"
)

// Gemini installs ACC's hooks into Gemini CLI's settings.json, whose hooks
// take Claude Code's shape under Gemini's own event names. Gemini runs
// hooks only in trusted folders.
type Gemini struct{}

const geminiMarker = " hook gemini"

// geminiEvents are the Gemini hook events ACC subscribes to. With no
// matcher, tool hooks fire for every tool. PreCompress is left out: Gemini
// fires it before every model call, compressing or not.
var geminiEvents = []hookEvent{
	{"SessionStart", false, 0},
	{"BeforeAgent", false, 0},
	{"BeforeTool", false, 0},
	{"AfterTool", false, 0},
	{"Notification", false, 0},
	{"AfterAgent", false, 0},
	{"SessionEnd", false, 0},
}

// Gemini's hook timeouts are in milliseconds.
var geminiHooks = hookFile{Events: geminiEvents, Marker: geminiMarker, Timeout: 5000}

func (*Gemini) Name() string { return gemini.Agent }

func (*Gemini) Detect() (Installation, bool) {
	bin, err := exec.LookPath("gemini")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	return Installation{Binary: bin, Version: strings.TrimSpace(string(out))}, true
}

// settingsPath: user scope uses ~/.gemini/settings.json, project scope the
// repo's .gemini/settings.json.
func (*Gemini) settingsPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".gemini", "settings.json"), nil
	}
	return gemini.SettingsPath(), nil
}

func (g *Gemini) Plan(opts Options) ([]Change, error)          { return g.plan(opts, true) }
func (g *Gemini) PlanUninstall(opts Options) ([]Change, error) { return g.plan(opts, false) }

func (g *Gemini) plan(opts Options, install bool) ([]Change, error) {
	path, err := g.settingsPath(opts)
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
	if err := geminiHooks.merge(&doc, opts.Bin, install); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// settings.json holds more than hooks: it is kept even when emptied.
	var next []byte
	if len(doc) > 0 || old != nil {
		if next, err = doc.marshalIndent(); err != nil {
			return nil, err
		}
	}
	if old != nil && sameJSON(old, next) {
		next = old
	}
	if old == nil && next == nil {
		return nil, nil
	}
	note := "hooks: " + strings.Join(geminiHooks.names(), ", ") + " (Gemini runs them in trusted folders)"
	if !install {
		note = "remove ACC hooks"
	}
	return []Change{{Path: path, Old: old, New: next, Note: note}}, nil
}
