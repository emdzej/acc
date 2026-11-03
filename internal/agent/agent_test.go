package agent

import "testing"

func TestRegistry(t *testing.T) {
	owner := map[string]string{} // ID or short name -> agent
	for _, a := range All() {
		for _, k := range []string{a.ID, a.Short} {
			if o, taken := owner[k]; k == "" || taken && o != a.ID {
				t.Errorf("%q: empty, or also used by %s", k, o)
			}
			owner[k] = a.ID
		}
		if a.Name == "" || a.Command == "" || a.Delivery == "" {
			t.Errorf("%s: incomplete %+v", a.ID, a)
		}
		// A session ACC starts must report in under the ID ACC gave it
		// (or claim it through $ACC_LAUNCH_ID).
		if a.CanStart() && !a.LaunchEnv {
			if args := a.StartArgs(Start{SessionID: "id-1"}); !contains(args, "id-1") {
				t.Errorf("%s: start args %v don't pass the session ID", a.ID, args)
			}
		}
		if a.CanResume() && !contains(a.ResumeArgs("id-2"), "id-2") {
			t.Errorf("%s: resume args don't pass the session ID", a.ID)
		}
	}
	if a, ok := Lookup("claude"); !ok || a.ID != Claude {
		t.Error("lookup by short name")
	}
	if a := Get("mystery"); a.CanResume() || a.CanStart() || a.Delivery != DeliverNone {
		t.Errorf("unknown agents only get monitoring: %+v", a)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
