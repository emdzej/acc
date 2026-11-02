package setup

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/config"
)

//go:embed opencode_plugin.js
var openCodePlugin string

// openCodeMarker starts every plugin file ACC writes; setup only ever
// replaces or removes files carrying it.
const openCodeMarker = "// acc-managed:"

// OpenCode installs ACC's plugin into OpenCode 2.x: a single file in its
// plugins directory, which OpenCode loads at startup. No config is edited.
type OpenCode struct{}

func (*OpenCode) Name() string { return "opencode" }

func (*OpenCode) Detect() (Installation, bool) {
	bin, err := exec.LookPath("opencode")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	return Installation{Binary: bin, Version: strings.TrimSpace(string(out))}, true
}

// pluginPath: user scope uses the global plugins dir, project scope the
// repo's .opencode/plugins.
func (*OpenCode) pluginPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".opencode", "plugins", "acc.js"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "opencode", "plugins", "acc.js"), nil
}

func (o *OpenCode) Plan(opts Options) ([]Change, error) {
	path, err := o.pluginPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readOwned(path)
	if err != nil {
		return nil, err
	}
	// The plugin reads the token at runtime; bake in where to find it, since
	// OpenCode may not see ACC_HOME / ACC_SERVER in its environment.
	src := strings.NewReplacer("__ACC_HOME__", jsString(config.Home()), "__ACC_SERVER__", jsString(config.ServerURL())).Replace(openCodePlugin)
	return []Change{{Path: path, Old: old, New: []byte(src), Note: "plugin: session events, usage, messages and /commands"}}, nil
}

func (o *OpenCode) PlanUninstall(opts Options) ([]Change, error) {
	path, err := o.pluginPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readOwned(path)
	if err != nil || old == nil {
		return nil, err
	}
	return []Change{{Path: path, Old: old, New: nil, Note: "remove plugin"}}, nil
}

// readOwned reads an existing file, refusing to touch one ACC didn't write.
func readOwned(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(b, []byte(openCodeMarker)) {
		return nil, fmt.Errorf("%s exists and wasn't written by ACC; move it aside and re-run", path)
	}
	return b, nil
}

// jsString escapes s for a double-quoted JS string literal.
func jsString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
