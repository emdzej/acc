// Package launch starts new agent sessions in a terminal window. A session
// is interactive, so ACC opens a terminal for it rather than running it
// itself; the session then reports back through its hooks like any other.
package launch

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emdzej/acc/internal/config"
)

// Terminals ACC knows how to open.
const (
	Ghostty  = "ghostty"
	Terminal = "terminal" // macOS Terminal.app
	ITerm    = "iterm"
	Tmux     = "tmux"
	Custom   = "custom" // run Config.TerminalCommand with $ACC_CWD / $ACC_CMD
)

// DefaultCommand starts Claude Code with ACC's message channel enabled.
const DefaultCommand = "claude --dangerously-load-development-channels server:acc"

// Config is the "launch" section of ~/.acc/config.json. It's read on each
// launch, so edits apply without restarting the daemon.
type Config struct {
	Terminal string `json:"terminal,omitempty"` // default: ghostty if installed, else terminal
	// Command is the agent command line; ACC appends --session-id, --name and the prompt.
	Command string `json:"command,omitempty"`
	// TerminalCommand is a shell command for Terminal == "custom".
	TerminalCommand string `json:"terminal_command,omitempty"`
}

func ConfigPath() string { return filepath.Join(config.Home(), "config.json") }

// Load reads the launch config, filling defaults.
func Load() (Config, error) {
	var doc struct {
		Launch Config `json:"launch"`
	}
	b, err := os.ReadFile(ConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &doc); err != nil {
			return Config{}, fmt.Errorf("%s: %w", ConfigPath(), err)
		}
	}
	c := doc.Launch
	if c.Command == "" {
		c.Command = DefaultCommand
	}
	if c.Terminal == "" {
		c.Terminal = Terminal
		if _, err := os.Stat("/Applications/Ghostty.app"); err == nil {
			c.Terminal = Ghostty
		}
	}
	return c, nil
}

// Spec is one session to start.
type Spec struct {
	SessionID string
	Cwd       string
	Name      string // shown in the agent's /resume list
	Prompt    string // optional first prompt
}

// NewSessionID returns a random UUIDv4, which Claude Code accepts for --session-id.
func NewSessionID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ResolveDir expands ~ and checks the directory exists.
func ResolveDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("directory required")
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// CommandLine is the shell command the terminal runs.
func (c Config) CommandLine(s Spec) string {
	cmd := c.Command + " --session-id " + Quote(s.SessionID)
	if s.Name != "" {
		cmd += " --name " + Quote(s.Name)
	}
	if s.Prompt != "" {
		cmd += " -- " + Quote(s.Prompt)
	}
	return cmd
}

// Start opens a terminal window running the session. It returns once the
// terminal has been asked to open; the session reports in via its hooks.
func (c Config) Start(s Spec) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	line := c.CommandLine(s)
	// A login, interactive shell gives the session the user's usual PATH and env.
	inDir := "cd " + Quote(s.Cwd) + " && exec " + line
	var cmd *exec.Cmd
	switch c.Terminal {
	case Ghostty:
		// Ghostty 1.3+ scripting opens a window in the running app; `open -na`
		// would start a separate Ghostty instance per session.
		cmd = exec.Command("osascript",
			"-e", `on run argv`,
			"-e", `tell application "Ghostty"`,
			"-e", `set cfg to new surface configuration`,
			"-e", `set initial working directory of cfg to (item 1 of argv)`,
			"-e", `set command of cfg to (item 2 of argv)`,
			"-e", `new window with configuration cfg`,
			"-e", `activate`,
			"-e", `end tell`,
			"-e", `end run`, s.Cwd, shell+" -lic "+Quote(line))
	case Terminal:
		cmd = exec.Command("osascript",
			"-e", `on run argv`,
			"-e", `tell application "Terminal" to do script (item 1 of argv)`,
			"-e", `tell application "Terminal" to activate`,
			"-e", `end run`, inDir)
	case ITerm:
		cmd = exec.Command("osascript",
			"-e", `on run argv`,
			"-e", `tell application "iTerm" to create window with default profile command (item 1 of argv)`,
			"-e", `tell application "iTerm" to activate`,
			"-e", `end run`, shell+" -lic "+Quote(inDir))
	case Tmux:
		if exec.Command("tmux", "has-session").Run() == nil {
			cmd = exec.Command("tmux", "new-window", "-c", s.Cwd, shell, "-lic", line)
		} else {
			cmd = exec.Command("tmux", "new-session", "-d", "-s", "acc", "-c", s.Cwd, shell, "-lic", line)
		}
	case Custom:
		if c.TerminalCommand == "" {
			return errors.New(`launch.terminal is "custom" but launch.terminal_command is empty`)
		}
		cmd = exec.Command("/bin/sh", "-c", c.TerminalCommand)
		cmd.Env = append(os.Environ(), "ACC_CWD="+s.Cwd, "ACC_CMD="+line, "ACC_SESSION_ID="+s.SessionID)
	default:
		return fmt.Errorf("unknown launch.terminal %q (ghostty, terminal, iterm, tmux, custom)", c.Terminal)
	}
	cmd.Dir = s.Cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", c.Terminal, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Quote single-quotes s for POSIX shells.
func Quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
