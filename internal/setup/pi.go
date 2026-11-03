package setup

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/pi"
)

//go:embed pi_extension.js
var piExtension string

// Pi installs ACC's extension into pi: a single file in its extensions
// directory, which pi discovers at startup. No config is edited.
type Pi struct{}

func (*Pi) Name() string { return pi.Agent }

func (*Pi) Detect() (Installation, bool) {
	bin, err := exec.LookPath("pi")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	return Installation{Binary: bin, Version: strings.TrimSpace(string(out))}, true
}

// extensionPath: user scope uses pi's global extensions directory, project
// scope the repo's .pi/extensions (loaded once the project is trusted).
func (*Pi) extensionPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".pi", "extensions", "acc.js"), nil
	}
	return filepath.Join(pi.Dir(), "extensions", "acc.js"), nil
}

func (p *Pi) Plan(opts Options) ([]Change, error) {
	path, err := p.extensionPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readOwned(path)
	if err != nil {
		return nil, err
	}
	// The extension reads the token at runtime; bake in where to find it,
	// since pi may not see ACC_HOME / ACC_SERVER in its environment.
	src := strings.NewReplacer("__ACC_HOME__", jsString(config.Home()), "__ACC_SERVER__", jsString(config.ServerURL())).Replace(piExtension)
	return []Change{{Path: path, Old: old, New: []byte(src), Note: "extension: session events and messages"}}, nil
}

func (p *Pi) PlanUninstall(opts Options) ([]Change, error) {
	path, err := p.extensionPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := readOwned(path)
	if err != nil || old == nil {
		return nil, err
	}
	return []Change{{Path: path, Old: old, New: nil, Note: "remove extension"}}, nil
}
