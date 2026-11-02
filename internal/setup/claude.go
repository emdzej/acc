package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hookMarker identifies hook entries owned by ACC, independent of the binary path.
const hookMarker = " hook claude-code"

// claudeEvents are the Claude Code hook events ACC subscribes to.
// Tool events take a matcher; "*" matches every tool.
var claudeEvents = []struct {
	Name    string
	Matcher bool
}{
	{"SessionStart", false},
	{"UserPromptSubmit", false},
	{"PreToolUse", true},
	{"PostToolUse", true},
	{"PostToolUseFailure", true},
	{"PermissionRequest", true},
	{"Notification", false},
	{"Stop", false},
	{"SubagentStart", false},
	{"SubagentStop", false},
	{"SessionEnd", false},
}

type Claude struct{}

func (*Claude) Name() string { return "claude-code" }

func (*Claude) Detect() (Installation, bool) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return Installation{}, false
	}
	out, _ := exec.Command(bin, "--version").Output()
	return Installation{Binary: bin, Version: strings.TrimSpace(string(out))}, true
}

// settingsPath: user scope edits ~/.claude/settings.json; project scope uses
// .claude/settings.local.json so absolute binary paths don't get committed.
func (*Claude) settingsPath(opts Options) (string, error) {
	if opts.Scope == ScopeProject {
		dir := opts.ProjectDir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, ".claude", "settings.local.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func (c *Claude) Plan(opts Options) ([]Change, error) {
	return c.plan(opts, true)
}

func (c *Claude) PlanUninstall(opts Options) ([]Change, error) {
	return c.plan(opts, false)
}

func (c *Claude) plan(opts Options, install bool) ([]Change, error) {
	path, err := c.settingsPath(opts)
	if err != nil {
		return nil, err
	}
	old, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	} else if err != nil {
		return nil, err
	}

	doc, err := parseOrdered(old)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	hooks := map[string][]hookGroup{}
	if raw, ok := doc.get("hooks"); ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return nil, fmt.Errorf("%s: unexpected hooks shape: %w", path, err)
		}
	}

	for _, e := range claudeEvents {
		hooks[e.Name] = stripACC(hooks[e.Name])
		if install {
			g := hookGroup{Hooks: []hookEntry{{
				Type:    "command",
				Command: shellQuote(opts.Bin) + hookMarker,
				Timeout: 5,
			}}}
			if e.Matcher {
				g.Matcher = "*"
			}
			hooks[e.Name] = append(hooks[e.Name], g)
		}
		if len(hooks[e.Name]) == 0 {
			delete(hooks, e.Name)
		}
	}

	if err := planStatusLine(&doc, opts, install && !opts.NoStatusLine); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if len(hooks) == 0 {
		doc.del("hooks")
	} else {
		raw, err := marshalNoEscape(hooks)
		if err != nil {
			return nil, err
		}
		doc.set("hooks", raw)
	}

	var next []byte
	if len(doc) > 0 || old != nil {
		if next, err = doc.marshalIndent(); err != nil {
			return nil, err
		}
	}
	// Re-indenting an untouched file is still a no-op, so compare normalized forms.
	if old != nil && sameJSON(old, next) {
		next = old
	}
	note := "hooks: " + strings.Join(eventNames(), ", ")
	if !opts.NoStatusLine {
		note += "; statusLine (plan limits, context)"
	}
	if !install {
		note = "remove ACC hooks"
	}
	return []Change{{Path: path, Old: old, New: next, Note: note}}, nil
}

// hookGroup / hookEntry mirror Claude Code's settings schema. Entries ACC
// didn't write are re-emitted byte-for-byte (raw), so no field or key order is lost.
type hookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
	raw     json.RawMessage
}

func (h *hookEntry) UnmarshalJSON(b []byte) error {
	type plain hookEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*h = hookEntry(p)
	h.raw = append(json.RawMessage(nil), b...)
	return nil
}

func (h hookEntry) MarshalJSON() ([]byte, error) {
	if h.raw != nil {
		return h.raw, nil
	}
	type plain hookEntry
	return marshalNoEscape(plain(h))
}

// marshalNoEscape keeps "&&", "<" and ">" readable instead of \u0026 etc.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// planStatusLine installs or removes ACC's statusLine command. A user's
// existing status line is kept by wrapping it: acc runs it via --chain and
// prints its output, and uninstall restores the original command.
func planStatusLine(doc *orderedDoc, opts Options, install bool) error {
	sl := map[string]json.RawMessage{}
	raw, exists := doc.get("statusLine")
	if exists {
		if err := json.Unmarshal(raw, &sl); err != nil {
			return fmt.Errorf("unexpected statusLine shape: %w", err)
		}
	}
	var cmd string
	if c, ok := sl["command"]; ok {
		json.Unmarshal(c, &cmd)
	}
	chain, ours := parseACCStatusLine(cmd)
	if exists && !ours {
		chain = cmd // a user's own status line: wrap it
	}

	switch {
	case install:
		next := shellQuote(opts.Bin) + statusLineMarker
		if chain != "" {
			next += " --chain " + shellQuote(chain)
		}
		if !exists {
			sl["type"] = json.RawMessage(`"command"`)
		}
		sl["command"], _ = marshalNoEscape(next)
	case ours && chain != "":
		sl["command"], _ = marshalNoEscape(chain)
	case ours:
		doc.del("statusLine")
		return nil
	default:
		return nil // not ours: leave it alone
	}
	b, err := marshalNoEscape(sl)
	if err != nil {
		return err
	}
	// Keep the user's original bytes when nothing actually changed.
	if exists && sameJSON(raw, b) {
		return nil
	}
	doc.set("statusLine", b)
	return nil
}

const statusLineMarker = " statusline"

// parseACCStatusLine recognizes `<…/acc> statusline [--chain <quoted cmd>]`.
func parseACCStatusLine(cmd string) (chain string, ours bool) {
	bin, rest := splitFirstToken(cmd)
	if filepath.Base(bin) != "acc" || !strings.HasPrefix(rest, statusLineMarker) {
		return "", false
	}
	rest = strings.TrimPrefix(rest, statusLineMarker)
	if rest == "" {
		return "", true
	}
	if !strings.HasPrefix(rest, " --chain ") {
		return "", false
	}
	return shellUnquote(strings.TrimPrefix(rest, " --chain ")), true
}

// splitFirstToken splits off a leading shellQuote'd or bare token.
func splitFirstToken(s string) (tok, rest string) {
	if strings.HasPrefix(s, "'") {
		for i := 1; i < len(s); i++ {
			if s[i] == '\'' && !strings.HasPrefix(s[i:], `'\''`) {
				return shellUnquote(s[:i+1]), s[i+1:]
			}
			if strings.HasPrefix(s[i:], `'\''`) {
				i += 3
			}
		}
		return "", ""
	}
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// shellUnquote reverses shellQuote.
func shellUnquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}

func stripACC(groups []hookGroup) []hookGroup {
	var out []hookGroup
	for _, g := range groups {
		var kept []hookEntry
		for _, h := range g.Hooks {
			if !strings.HasSuffix(h.Command, hookMarker) {
				kept = append(kept, h)
			}
		}
		if len(kept) > 0 {
			g.Hooks = kept
			out = append(out, g)
		}
	}
	return out
}

func eventNames() []string {
	out := make([]string, len(claudeEvents))
	for i, e := range claudeEvents {
		out[i] = e.Name
	}
	return out
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// orderedDoc is a top-level JSON object that preserves key order and leaves
// values it doesn't touch byte-for-byte as the user wrote them.
type orderedDoc []kv

type kv struct {
	Key string
	Val json.RawMessage
}

func parseOrdered(b []byte) (orderedDoc, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return orderedDoc{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("settings file is not a JSON object")
	}
	var doc orderedDoc
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		doc = append(doc, kv{key, raw})
	}
	return doc, nil
}

func (d orderedDoc) get(k string) (json.RawMessage, bool) {
	for _, e := range d {
		if e.Key == k {
			return e.Val, true
		}
	}
	return nil, false
}

func (d *orderedDoc) set(k string, v json.RawMessage) {
	for i, e := range *d {
		if e.Key == k {
			(*d)[i].Val = v
			return
		}
	}
	*d = append(*d, kv{k, v})
}

func (d *orderedDoc) del(k string) {
	for i, e := range *d {
		if e.Key == k {
			*d = append((*d)[:i], (*d)[i+1:]...)
			return
		}
	}
}

func (d orderedDoc) marshalIndent() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range d {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(e.Key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(e.Val)
	}
	buf.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}
