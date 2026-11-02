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
	"github.com/emdzej/acc/internal/model"
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
	// Tmux hosts sessions in tmux, with Terminal opening a window attached
	// to it: sessions survive closed windows and accept slash commands.
	Tmux bool `json:"tmux,omitempty"`
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
		c.Terminal = defaultTerminal()
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

// Host is where a launched session runs, so ACC can type into it (slash
// commands) and bring it back: a tmux session name or a Ghostty terminal ID.
type Host = model.Host

const (
	HostTmux    = "tmux"
	HostGhostty = "ghostty"
)

// Start runs the session and returns where it lives. With Tmux set, the
// session runs in its own detached tmux session (surviving closed windows)
// and Terminal, unless it is "tmux" itself, opens a window attached to it.
// It returns once the window was asked to open; the session reports in via its hooks.
func (c Config) Start(s Spec) (Host, error) {
	line := c.CommandLine(s)
	if c.Tmux || c.Terminal == Tmux {
		name := "acc-" + s.SessionID[:8]
		// A login, interactive shell gives the session the user's usual PATH and env.
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", s.Cwd, "--", shell(), "-lic", line).CombinedOutput(); err != nil {
			return Host{}, fmt.Errorf("tmux: %v: %s", err, strings.TrimSpace(string(out)))
		}
		h := Host{Kind: HostTmux, Ref: name}
		if c.Terminal == Tmux {
			return h, nil // headless: `tmux attach -t acc-…` or acc attach
		}
		_, err := c.openWindow(s.Cwd, attachLine(name), s.SessionID)
		return h, err
	}
	ref, err := c.openWindow(s.Cwd, line, s.SessionID)
	if err != nil || ref == "" {
		return Host{}, err
	}
	return Host{Kind: HostGhostty, Ref: ref}, nil
}

// Attach brings a session back on screen: a new window attached to its tmux
// session, or its Ghostty window raised.
func (c Config) Attach(h Host, cwd string) error {
	switch h.Kind {
	case HostTmux:
		if exec.Command("tmux", "has-session", "-t", "="+h.Ref).Run() != nil {
			return fmt.Errorf("tmux session %s is gone", h.Ref)
		}
		if c.Terminal == Tmux { // headless preference: still need some window to attach from
			c.Terminal = defaultTerminal()
		}
		_, err := c.openWindow(cwd, attachLine(h.Ref), "")
		return err
	case HostGhostty:
		return osascript([]string{
			`on run argv`, `tell application "Ghostty"`,
			`set t to first terminal whose id is (item 1 of argv)`,
			`focus t`, `activate`, `end tell`, `end run`}, h.Ref)
	}
	return errors.New("session has no known window to bring back")
}

// Type enters text into the session's terminal as if typed, then presses
// Enter. This is how slash commands reach a session: channel and hook
// messages are prompts and never run commands.
func Type(h Host, text string) error {
	switch h.Kind {
	case HostTmux:
		t := "=" + h.Ref + ":"
		if out, err := exec.Command("tmux", "send-keys", "-t", t, "-l", "--", text).CombinedOutput(); err != nil {
			return fmt.Errorf("tmux: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return exec.Command("tmux", "send-keys", "-t", t, "Enter").Run()
	case HostGhostty:
		return osascript([]string{
			`on run argv`, `tell application "Ghostty"`,
			`set t to first terminal whose id is (item 1 of argv)`,
			`input text (item 2 of argv) to t`,
			`send key "enter" to t`, `end tell`, `end run`}, h.Ref, text)
	}
	return errors.New("ACC can only type into sessions it started in tmux or Ghostty")
}

// Alive reports whether the host still exists.
func Alive(h Host) bool {
	switch h.Kind {
	case HostTmux:
		return exec.Command("tmux", "has-session", "-t", "="+h.Ref).Run() == nil
	case HostGhostty:
		out, err := exec.Command("osascript", "-e", `on run argv`, "-e", `tell application "Ghostty" to return exists (first terminal whose id is (item 1 of argv))`, "-e", `end run`, h.Ref).Output()
		return err == nil && strings.TrimSpace(string(out)) == "true"
	}
	return false
}

func attachLine(name string) string { return "exec tmux attach -t " + Quote("="+name) }

func shell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/zsh"
}

func defaultTerminal() string {
	if _, err := os.Stat("/Applications/Ghostty.app"); err == nil {
		return Ghostty
	}
	return Terminal
}

// openWindow opens a terminal window running line in a login shell. For
// Ghostty it returns the new terminal's ID.
func (c Config) openWindow(cwd, line, sessionID string) (string, error) {
	sh := shell()
	inDir := line
	if cwd != "" {
		inDir = "cd " + Quote(cwd) + " && " + line
	}
	switch c.Terminal {
	case Ghostty:
		// Ghostty 1.3+ scripting opens a window in the running app; `open -na`
		// would start a separate Ghostty instance per session.
		out, err := exec.Command("osascript",
			"-e", `on run argv`,
			"-e", `tell application "Ghostty"`,
			"-e", `set cfg to new surface configuration`,
			"-e", `if (item 1 of argv) is not "" then set initial working directory of cfg to (item 1 of argv)`,
			"-e", `set command of cfg to (item 2 of argv)`,
			"-e", `set w to new window with configuration cfg`,
			"-e", `activate`,
			"-e", `return id of focused terminal of selected tab of w`,
			"-e", `end tell`,
			"-e", `end run`, cwd, sh+" -lic "+Quote(line)).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("ghostty: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	case Terminal:
		return "", osascript([]string{`on run argv`,
			`tell application "Terminal" to do script (item 1 of argv)`,
			`tell application "Terminal" to activate`, `end run`}, inDir)
	case ITerm:
		return "", osascript([]string{`on run argv`,
			`tell application "iTerm" to create window with default profile command (item 1 of argv)`,
			`tell application "iTerm" to activate`, `end run`}, sh+" -lic "+Quote(inDir))
	case Custom:
		if c.TerminalCommand == "" {
			return "", errors.New(`launch.terminal is "custom" but launch.terminal_command is empty`)
		}
		cmd := exec.Command("/bin/sh", "-c", c.TerminalCommand)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "ACC_CWD="+cwd, "ACC_CMD="+line, "ACC_SESSION_ID="+sessionID)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("custom terminal: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown launch.terminal %q (ghostty, terminal, iterm, tmux, custom)", c.Terminal)
}

func osascript(lines []string, args ...string) error {
	var a []string
	for _, l := range lines {
		a = append(a, "-e", l)
	}
	if out, err := exec.Command("osascript", append(a, args...)...).CombinedOutput(); err != nil {
		return fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Quote single-quotes s for POSIX shells.
func Quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Paint colours a tmux-hosted session's status bar and active pane border
// with the session's colour and shows its title there, so its window is
// recognisable at a glance. An empty colour restores tmux's defaults.
func Paint(h Host, color, title string) error {
	if h.Kind != HostTmux {
		return nil
	}
	t := "=" + h.Ref + ":" // set-option needs the session:window form for an exact match
	set := func(args ...string) error {
		out, err := exec.Command("tmux", append([]string{"set-option", "-t", t}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("tmux: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if color == "" {
		for _, o := range []string{"status-style", "pane-active-border-style", "status-left", "status-left-length"} {
			if err := set("-u", o); err != nil {
				return err
			}
		}
		return nil
	}
	fg := "#ffffff"
	if luminance(color) > 0.55 {
		fg = "#000000"
	}
	// tmux expands #() and #[] in status formats: keep titles literal.
	label := strings.NewReplacer("#", "##").Replace(title)
	for _, kv := range [][2]string{
		{"status-style", "bg=" + color + ",fg=" + fg},
		{"pane-active-border-style", "fg=" + color},
		{"status-left-length", "60"},
		{"status-left", " " + label + " │ "},
	} {
		if err := set(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// luminance is a #rrggbb colour's relative brightness, 0..1.
func luminance(hex string) float64 {
	var r, g, b int
	if _, err := fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0
	}
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
}
