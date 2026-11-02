// Package setup installs ACC integrations (hooks, MCP servers, plugins) into agents.
//
// Rules every adapter follows:
//   - merge, never overwrite: only ACC-marked entries are added/replaced/removed
//   - idempotent: running setup twice yields no diff
//   - back up the original, then write atomically
//   - no secrets in agent config; hooks call the acc binary, which reads its own credentials
package setup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type Options struct {
	Scope      Scope
	ProjectDir string // used with ScopeProject
	Bin        string // absolute path to the acc binary written into hook commands
	// NoStatusLine skips (and on install, removes) ACC's status line integration.
	NoStatusLine bool
}

// Installation describes a detected agent.
type Installation struct {
	Version string
	Binary  string
}

// Change is a full-file rewrite of one config file.
type Change struct {
	Path string
	Old  []byte // nil when the file does not exist
	New  []byte
	Note string
}

func (c Change) Noop() bool { return bytes.Equal(c.Old, c.New) }

type Adapter interface {
	Name() string
	Detect() (Installation, bool)
	Plan(opts Options) ([]Change, error)
	PlanUninstall(opts Options) ([]Change, error)
}

func Adapters() []Adapter { return []Adapter{&Claude{}} }

func Lookup(name string) (Adapter, bool) {
	for _, a := range Adapters() {
		if a.Name() == name || (name == "claude" && a.Name() == "claude-code") {
			return a, true
		}
	}
	return nil, false
}

// Apply backs up and atomically writes each non-noop change.
func Apply(changes []Change) error {
	ts := time.Now().Format("20060102-150405")
	for _, c := range changes {
		if c.Noop() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if c.Old != nil {
			if fi, err := os.Stat(c.Path); err == nil {
				mode = fi.Mode().Perm()
			}
			if err := os.WriteFile(c.Path+".acc-bak-"+ts, c.Old, mode); err != nil {
				return fmt.Errorf("backup %s: %w", c.Path, err)
			}
		}
		tmp, err := os.CreateTemp(filepath.Dir(c.Path), ".acc-tmp-*")
		if err != nil {
			return err
		}
		_, werr := tmp.Write(c.New)
		cerr := tmp.Close()
		if werr != nil || cerr != nil {
			os.Remove(tmp.Name())
			return fmt.Errorf("write %s: %v %v", c.Path, werr, cerr)
		}
		os.Chmod(tmp.Name(), mode)
		if err := os.Rename(tmp.Name(), c.Path); err != nil {
			os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}

// ResolveBin picks the acc path to embed in hook commands, rejecting
// ephemeral `go run` binaries that would vanish after the command exits.
func ResolveBin(override string) (string, error) {
	bin := override
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		bin = exe
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return "", err
	}
	if strings.Contains(abs, "go-build") || strings.HasPrefix(abs, os.TempDir()) {
		return "", fmt.Errorf("acc is running from a temporary path (%s); install it (go install ./cmd/acc) or pass --bin", abs)
	}
	return abs, nil
}

// Diff renders a minimal line diff of a change.
func Diff(c Change) string {
	a := splitLines(c.Old)
	b := splitLines(c.New)
	// LCS table; config files are small enough for O(n*m).
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", c.Path, c.Path)
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case j < len(b) && (i == len(a) || dp[i][j+1] >= dp[i+1][j]):
			fmt.Fprintf(&out, "+%s\n", b[j])
			j++
		default:
			fmt.Fprintf(&out, "-%s\n", a[i])
			i++
		}
	}
	return out.String()
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
