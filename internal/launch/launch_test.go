package launch

import (
	"os/exec"
	"regexp"
	"testing"
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
