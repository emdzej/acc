package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emdzej/acc/internal/model"
)

func TestAgentsAPI(t *testing.T) {
	h := testServer(t, Access{})
	auth := map[string]string{"Authorization": "Bearer local-token"}
	w := do(h, "GET", "http://127.0.0.1:7317/api/v1/agents", auth, "")
	var got []model.AgentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got) < 2 {
		t.Fatalf("agents: %d %s", w.Code, w.Body)
	}
	if a := got[0]; a.ID != "claude-code" || !a.Start || !a.Resume || a.Delivery != "hook" {
		t.Errorf("claude: %+v", a)
	}
	// Launching is refused, before any window opens, for agents ACC can't start.
	auth["Content-Type"] = "application/json"
	for _, ag := range []string{"opencode", "nope"} {
		w := do(h, "POST", "http://127.0.0.1:7317/api/v1/sessions", auth, `{"agent":"`+ag+`","cwd":"`+t.TempDir()+`"}`)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "can't start") {
			t.Errorf("launch %s: %d %s", ag, w.Code, w.Body)
		}
	}
}
