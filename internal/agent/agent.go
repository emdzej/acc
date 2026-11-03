// Package agent is ACC's registry of the coding agents (harnesses) it
// supports: what each one is called, how to start and resume its sessions,
// and how messages reach them. Everything agent-specific that isn't about
// parsing an agent's own files or events is declared here, so the server,
// TUI and web UI ask the registry instead of comparing agent names.
//
// Adding an agent touches a few packages; see site/docs/adding-an-agent.md.
package agent

import "github.com/emdzej/acc/internal/model"

// IDs of the built-in agents, as sessions record them.
const (
	Claude   = "claude-code"
	OpenCode = "opencode"
	Pi       = "pi"
)

// Delivery is how ACC's messages reach a running session.
type Delivery string

const (
	// DeliverHook: queued messages ride on the next hook that can inject
	// them (a prompt, a tool call, the end of a turn).
	DeliverHook Delivery = "hook"
	// DeliverPlugin: ACC's plugin or extension inside the agent claims and
	// sends them right away, waking an idle session.
	DeliverPlugin Delivery = "plugin"
	// DeliverNone: only sessions ACC hosts in tmux, by typing into them.
	DeliverNone Delivery = "none"
)

// Start is a new session for StartArgs.
type Start struct {
	SessionID string // chosen by ACC, so the session's events match the row it prepared
	Name      string // the agent's session name, if it has one
	Prompt    string // first prompt, optional
}

// Agent describes one harness.
type Agent struct {
	ID    string // as sessions record it, e.g. "claude-code"
	Name  string // for people, e.g. "Claude Code"
	Short string // a short tag for lists, e.g. "claude"
	// Command is the default command line that runs the agent; the
	// launch config can override it per agent (launch.commands).
	Command string
	// ResumeArgs are appended to Command to continue session id; nil when
	// ACC can't resume this agent's sessions.
	ResumeArgs func(id string) []string
	// StartArgs are appended to Command to start a new session; nil when
	// ACC can't start one (the agent can't take a session ID from ACC).
	StartArgs func(s Start) []string
	// Delivery is how messages reach a running session.
	Delivery Delivery
	// QueueSlash: the agent's plugin runs /commands from the message queue,
	// so they needn't be typed into a terminal.
	QueueSlash bool
	// Unconnected is shown for a live session that can't receive messages
	// right now, with what to do about it.
	Unconnected string
}

// CanResume reports whether ACC can reopen the agent's sessions.
func (a Agent) CanResume() bool { return a.ResumeArgs != nil }

// CanStart reports whether ACC can start new sessions of the agent.
func (a Agent) CanStart() bool { return a.StartArgs != nil }

// Info is the agent's public description, for the API.
func (a Agent) Info() model.AgentInfo {
	return model.AgentInfo{ID: a.ID, Name: a.Name, Short: a.Short, Resume: a.CanResume(), Start: a.CanStart(),
		Delivery: string(a.Delivery), Unconnected: a.Unconnected}
}

var registry = []Agent{
	{
		ID: Claude, Name: "Claude Code", Short: "claude",
		Command:    "claude --dangerously-load-development-channels server:acc",
		ResumeArgs: func(id string) []string { return []string{"--resume", id} },
		StartArgs: func(s Start) []string {
			args := []string{"--session-id", s.SessionID}
			if s.Name != "" {
				args = append(args, "--name", s.Name)
			}
			if s.Prompt != "" {
				args = append(args, "--", s.Prompt)
			}
			return args
		},
		Delivery:    DeliverHook,
		Unconnected: "messages wait for a hook (start with acc claude)",
	},
	{
		ID: OpenCode, Name: "OpenCode", Short: "opencode",
		Command:     "opencode",
		ResumeArgs:  func(id string) []string { return []string{"--session", id} },
		Delivery:    DeliverPlugin,
		QueueSlash:  true,
		Unconnected: "ACC plugin not connected (acc setup --agent opencode)",
	},
	{
		ID: Pi, Name: "pi", Short: "pi",
		Command:    "pi",
		ResumeArgs: func(id string) []string { return []string{"--session", id} },
		StartArgs: func(s Start) []string {
			args := []string{"--session-id", s.SessionID}
			if s.Name != "" {
				args = append(args, "--name", s.Name)
			}
			if s.Prompt != "" {
				args = append(args, "--", s.Prompt)
			}
			return args
		},
		Delivery:    DeliverPlugin,
		QueueSlash:  true,
		Unconnected: "ACC extension not loaded (acc setup --agent pi, then restart pi)",
	},
}

// All lists the supported agents, Claude Code first.
func All() []Agent { return registry }

// Lookup finds an agent by ID or short name.
func Lookup(id string) (Agent, bool) {
	for _, a := range registry {
		if a.ID == id || a.Short == id {
			return a, true
		}
	}
	return Agent{}, false
}

// Get is Lookup for an ID read back from a session; an unknown agent gets
// a description that supports nothing beyond monitoring.
func Get(id string) Agent {
	if a, ok := Lookup(id); ok {
		return a
	}
	return Agent{ID: id, Name: id, Short: id, Delivery: DeliverNone}
}

// Startable lists the agents ACC can start sessions of.
func Startable() []Agent {
	var out []Agent
	for _, a := range registry {
		if a.CanStart() {
			out = append(out, a)
		}
	}
	return out
}
