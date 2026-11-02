package channel

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ServerName is the MCP server name `acc setup` registers; sessions opt in
// with --dangerously-load-development-channels server:acc.
const ServerName = "acc"

const maxAncestors = 6

// claudeSession is the subset of Claude Code's ~/.claude/sessions/<pid>.json registry entry ACC needs.
type claudeSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// findClaudePID walks up from our parent to the Claude Code process: the
// first ancestor with a session registry entry.
func findClaudePID() int {
	pid := os.Getppid()
	for i := 0; i < maxAncestors && pid > 1; i++ {
		if _, err := os.Stat(registryPath(pid)); err == nil {
			return pid
		}
		pid = parentOf(pid)
	}
	return 0
}

func registryPath(pid int) string {
	return filepath.Join(claudeDir(), "sessions", strconv.Itoa(pid)+".json")
}

// sessionID re-reads the registry each time: /clear and /resume switch the
// session id while the process (and this channel) stays the same.
func sessionID(pid int) string {
	b, err := os.ReadFile(registryPath(pid))
	if err != nil {
		return ""
	}
	var s claudeSession
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.SessionID
}

func parentOf(pid int) int {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func argsOf(pid int) []string {
	out, err := exec.Command("ps", "-ww", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// optedIn reports whether the Claude command line registers this server as a
// channel. Claude Code drops channel events silently otherwise, so ACC must
// not claim messages it can't deliver; they stay queued for the hooks.
func optedIn(args []string) bool {
	inFlag, found := false, false
	for _, a := range args {
		flag, val, hasVal := strings.Cut(a, "=")
		switch {
		case flag == "--dangerously-load-development-channels" || flag == "--channels":
			inFlag = true
			if !hasVal {
				continue
			}
			a = val
		case strings.HasPrefix(a, "-"):
			inFlag = false
			continue
		}
		if !inFlag {
			continue
		}
		for _, v := range strings.Split(a, ",") {
			if v == "server:"+ServerName {
				found = true
			}
		}
	}
	if !found {
		return false
	}
	for _, a := range args {
		if a == "-p" || a == "--print" {
			return false // non-interactive sessions ignore channel flags
		}
	}
	return true
}
