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
	line, err := c.CommandLine(s)
	if err != nil {
		t.Fatal(err)
	}
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
	c := Config{Command: "claude"}
	for _, tc := range []struct {
		spec Spec
		want string
	}{
		{Spec{SessionID: "7116efd3-730d", Resume: true}, "claude --resume 7116efd3-730d"},
		{Spec{SessionID: "ses_19cb", Resume: true, Agent: "opencode"}, "opencode --session ses_19cb"},
	} {
		if got, err := c.CommandLine(tc.spec); err != nil || got != tc.want {
			t.Errorf("%+v: %q, %v; want %q", tc.spec, got, err, tc.want)
		}
	}
	// Overrides by agent ID or short name; ACC still appends its arguments.
	c = Config{Commands: map[string]string{"opencode": "oc --pure"}}
	if got, _ := c.CommandLine(Spec{SessionID: "ses_1", Resume: true, Agent: "opencode"}); got != "oc --pure --session ses_1" {
		t.Errorf("override: %s", got)
	}
	if got, _ := c.CommandLine(Spec{SessionID: "x", Resume: true}); got != DefaultCommand+" --resume x" {
		t.Errorf("default: %s", got)
	}
	// Codex picks its own IDs: the session claims ACC's registration through the environment.
	if got, _ := c.CommandLine(Spec{SessionID: "id-1", Agent: "codex", Prompt: "fix it"}); got != "ACC_LAUNCH_ID=id-1 codex -- 'fix it'" {
		t.Errorf("codex start: %s", got)
	}
	if got, _ := c.CommandLine(Spec{SessionID: "019a", Agent: "codex", Resume: true}); got != "codex resume 019a" {
		t.Errorf("codex resume: %s", got)
	}
	if _, err := c.CommandLine(Spec{SessionID: "ses_1", Agent: "opencode"}); err == nil {
		t.Error("ACC can't start OpenCode sessions")
	}
	if _, err := c.CommandLine(Spec{SessionID: "x", Resume: true, Agent: "nope"}); err == nil {
		t.Error("unknown agents can't be resumed")
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
