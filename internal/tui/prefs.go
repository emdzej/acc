package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/emdzej/acc/internal/config"
)

// prefs are display toggles that persist across TUI runs.
type prefs struct {
	ShowCost  bool `json:"show_cost"`
	HideEnded bool `json:"hide_ended"`
}

func prefsPath() string { return filepath.Join(config.Home(), "tui.json") }

func loadPrefs() prefs {
	p := prefs{ShowCost: true}
	if b, err := os.ReadFile(prefsPath()); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

// save is best effort: a read-only home shouldn't break the dashboard.
func (p prefs) save() {
	b, _ := json.MarshalIndent(p, "", "  ")
	os.MkdirAll(config.Home(), 0o700)
	os.WriteFile(prefsPath(), append(b, '\n'), 0o600)
}
