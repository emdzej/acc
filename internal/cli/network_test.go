package cli

import (
	"strings"
	"testing"
)

func TestConfirmExposure(t *testing.T) {
	for _, c := range []struct {
		name, auth, answer string
		yes, interactive   bool
		ok                 bool
	}{
		{"typed yes", authKey, "yes\n", false, true, true},
		{"typed YES", authKey, " YES \n", false, true, true},
		{"typed no", authKey, "no\n", false, true, false},
		{"no auth needs its own words", authNone, "yes\n", false, true, false},
		{"no auth typed", authNone, "no auth\n", false, true, true},
		{"unattended", authKey, "", false, false, false},
		{"unattended with --yes", authNone, "", true, false, true},
	} {
		o := serveOpts{auth: c.auth, yes: c.yes}
		var out strings.Builder
		err := confirmFrom("0.0.0.0:7317", &o, strings.NewReader(c.answer), &out, c.interactive)
		if (err == nil) != c.ok || o.approved != c.ok {
			t.Errorf("%s: err=%v approved=%v", c.name, err, o.approved)
		}
		if !strings.Contains(out.String(), "running code as you") {
			t.Errorf("%s: no warning shown", c.name)
		}
	}
}

func TestStartDaemonNeedsApproval(t *testing.T) {
	t.Setenv("ACC_HOME", t.TempDir())
	if _, err := startDaemon("0.0.0.0:0", nil, serveOpts{auth: authKey}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("unapproved exposure started: %v", err)
	}
	if _, err := startDaemon("127.0.0.1:0", nil, serveOpts{auth: authNone}); err == nil {
		t.Fatal("unapproved no-auth started")
	}
}
