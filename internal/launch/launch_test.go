package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandLineRoundTrip(t *testing.T) {
	c := Config{Command: DefaultCommand}
	s := Spec{SessionID: NewSessionID(), Name: "it's mine", Prompt: "fix $HOME `x` \"y\""}
	line := c.CommandLine(s)
	// Let the shell parse it: echo each argv element on its own line.
	out, err := exec.Command("/bin/sh", "-c", "set -- "+line+`; for a in "$@"; do printf '%s\n' "$a"; done`).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := "claude\n--dangerously-load-development-channels\nserver:acc\n--session-id\n" + s.SessionID +
		"\n--name\nit's mine\n--\nfix $HOME `x` \"y\"\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestNewSessionID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if id := NewSessionID(); !re.MatchString(id) {
		t.Errorf("bad uuid %s", id)
	}
}

func TestResumeCommandLine(t *testing.T) {
	c := Config{Command: "claude", OpenCodeCommand: "opencode"}
	if got := c.CommandLine(Spec{SessionID: "7116efd3-730d", Resume: true}); got != "claude --resume 7116efd3-730d" {
		t.Errorf("claude: %s", got)
	}
	if got := c.CommandLine(Spec{SessionID: "ses_19cb", Resume: true, Agent: "opencode"}); got != "opencode --session ses_19cb" {
		t.Errorf("opencode: %s", got)
	}
}

func TestEmulatorArgs(t *testing.T) {
	argv := []string{"/bin/sh", "-lic", "claude"}
	for app, want := range map[string]string{
		"/usr/bin/xdg-terminal-exec":   "/bin/sh -lic claude",
		"/usr/bin/gnome-terminal":      "-- /bin/sh -lic claude",
		"/usr/bin/x-terminal-emulator": "-e /bin/sh -lic claude",
		"kitty":                        "-e /bin/sh -lic claude",
	} {
		if got := strings.Join(emulatorArgs(app, argv), " "); got != want {
			t.Errorf("%s: %q, want %q", app, got, want)
		}
	}
}

func TestDefaultTerminal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		if got := defaultTerminal(); got != Exec {
			t.Errorf("got %s, want exec", got)
		}
		return
	}
	for prog, want := range map[string]string{"ghostty": Ghostty, "Apple_Terminal": Terminal, "iTerm.app": ITerm} {
		t.Setenv("TERM_PROGRAM", prog)
		if got := defaultTerminal(); got != want {
			t.Errorf("TERM_PROGRAM=%s: %s, want %s", prog, got, want)
		}
	}
}

func TestExecTerminal(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ran")
	// A fake emulator that records its arguments.
	app := filepath.Join(dir, "fake-term")
	os.WriteFile(app, []byte("#!/bin/sh\necho \"$PWD|$*\" > "+out+"\n"), 0o755)
	c := Config{Terminal: Exec, TerminalApp: app}
	if err := c.Shell(dir); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if b, err := os.ReadFile(out); err == nil {
			got := strings.TrimSpace(string(b))
			pwd, args, _ := strings.Cut(got, "|")
			p1, _ := filepath.EvalSymlinks(pwd)
			p2, _ := filepath.EvalSymlinks(dir)
			if p1 != p2 || !strings.HasPrefix(args, "-e ") || !strings.Contains(args, "-lic exec") {
				t.Errorf("ran %q", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("emulator never ran")
}
