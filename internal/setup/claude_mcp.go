package setup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// Command is a setup step run through the agent's own CLI rather than a file
// edit, for config the agent rewrites concurrently (e.g. ~/.claude.json).
type Command struct {
	Note string
	Argv []string
}

func (c Command) Run() error {
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// Commander is implemented by adapters that also need Commands.
type Commander interface {
	PlanCommands(opts Options, uninstall bool) ([]Command, error)
}

// ChannelServer is the MCP server name ACC registers for channel delivery.
const ChannelServer = "acc"

type mcpServer struct {
	Type    string   `json:"type,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// PlanCommands registers `acc mcp` as a user-scope MCP server, the channel
// that pushes ACC messages into sessions (and wakes idle ones). It's always
// user scope: project .mcp.json servers need per-project approval.
func (c *Claude) PlanCommands(opts Options, uninstall bool) ([]Command, error) {
	inst, ok := c.Detect()
	if !ok {
		return nil, nil
	}
	cur, exists := registeredMCP()
	want := mcpServer{Type: "stdio", Command: opts.Bin, Args: []string{"mcp"}}
	remove := Command{"remove MCP server " + ChannelServer, []string{inst.Binary, "mcp", "remove", "--scope", "user", ChannelServer}}
	if uninstall || opts.NoChannel {
		if exists {
			return []Command{remove}, nil
		}
		return nil, nil
	}
	if exists && cur.Command == want.Command && slices.Equal(cur.Args, want.Args) {
		return nil, nil
	}
	b, _ := json.Marshal(want)
	add := Command{"register MCP server " + ChannelServer + " (message channel)", []string{inst.Binary, "mcp", "add-json", "--scope", "user", ChannelServer, string(b)}}
	if exists {
		return []Command{remove, add}, nil
	}
	return []Command{add}, nil
}

// registeredMCP reads (never writes) the user-scope registration.
func registeredMCP() (mcpServer, bool) {
	path := os.Getenv("CLAUDE_CONFIG_DIR")
	if path != "" {
		path = filepath.Join(path, ".claude.json")
	} else {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".claude.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return mcpServer{}, false
	}
	var doc struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return mcpServer{}, false
	}
	s, ok := doc.MCPServers[ChannelServer]
	return s, ok
}
