package cli

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/setup"
	"github.com/emdzej/acc/internal/tui"
)

func tuiCmd() *cobra.Command {
	var serve string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Terminal dashboard (starts an embedded server if none is running)",
		Long: `Terminal dashboard.

--serve=auto (default) embeds the ACC server in this process when ACC_SERVER is
local and nothing is listening there, so one command is enough. The embedded
server stops when you quit: hooks fired afterwards are spooled and replayed on
the next start, but messages can't be delivered while nothing is running.
Run ` + "`acc serve`" + ` separately if you want ACC always on.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, local := localAddr(config.ServerURL())
			var embed bool
			switch serve {
			case "auto":
				embed = local && !listening(addr)
			case "always":
				if !local {
					return fmt.Errorf("--serve=always needs a local ACC_SERVER, got %s", config.ServerURL())
				}
				embed = true
			case "never":
			default:
				return fmt.Errorf("--serve must be auto, always or never")
			}

			if embed {
				if err := os.MkdirAll(config.Home(), 0o700); err != nil {
					return err
				}
				// The TUI owns the terminal, so server logs go to a file.
				logPath := filepath.Join(config.Home(), "serve.log")
				logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				defer logf.Close()
				d, err := startDaemon(addr, slog.New(slog.NewTextHandler(logf, nil)))
				if err != nil {
					return fmt.Errorf("start embedded server: %w", err)
				}
				defer d.Close()
			}

			c, err := newClient()
			if err != nil {
				return err
			}
			return tui.Run(c, tui.Options{Embedded: embed})
		},
	}
	cmd.Flags().StringVar(&serve, "serve", "auto", "auto | always | never: embed the ACC server in this process")
	return cmd
}

// localAddr returns host:port for a plain-HTTP loopback server URL.
func localAddr(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return "", false
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "80")
	}
	return addr, isLoopback(addr)
}

// listening reports whether anything accepts connections on addr. Any
// listener counts: we must not try to bind a port that is already taken.
func listening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func sessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"ls"},
		Short:   "List sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			ss, err := c.Sessions(cx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tAGENT\tTITLE\tSTATUS\tLAST TOOL\tSEEN\tGROUPS\tQUEUED")
			for _, s := range ss {
				names := make([]string, len(s.Groups))
				for i, g := range s.Groups {
					names[i] = g.Name
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n", s.ID[:min(8, len(s.ID))], s.Agent, s.Title, s.Status,
					s.LastTool, time.Since(time.UnixMilli(s.LastSeen)).Round(time.Second), strings.Join(names, ","), s.PendingCount)
			}
			return w.Flush()
		},
	}
}

func sendCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "send [session-id-prefix] <message>",
		Short: "Queue a message for a session (or --group); delivered when it next acts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := model.SendRequest{Group: group}
			if group == "" {
				if len(args) < 2 {
					return fmt.Errorf("usage: acc send <session> <message> | acc send --group <name> <message>")
				}
				req.SessionID, args = args[0], args[1:]
			}
			req.Body = strings.Join(args, " ")
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			resp, err := c.Send(cx, req)
			if err != nil {
				return err
			}
			for _, m := range resp.Queued {
				fmt.Printf("queued #%d for %s\n", m.ID, m.SessionID)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "send to every live session in this group")
	return cmd
}

func colorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "color <session-id-prefix> <#rrggbb|none>",
		Short: "Set a session's own colour (none reverts to its group colour)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			color := args[1]
			if color == "none" {
				color = ""
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			return c.SetSessionColor(cx, args[0], color)
		},
	}
}

func newCmd() *cobra.Command {
	var req model.LaunchRequest
	cmd := &cobra.Command{
		Use:   "new [dir]",
		Short: "Start a new Claude Code session in a terminal window (default dir: current)",
		Long: `Opens a terminal window running a new Claude Code session with ACC's
message channel enabled. The session is pre-registered, so --group, --title
and --color apply from the start.

The terminal and command come from the "launch" section of ~/.acc/config.json:

  {"launch": {"terminal": "ghostty",
              "command": "claude --dangerously-load-development-channels server:acc"}}

terminal: ghostty (default if installed), terminal, iterm, tmux, or custom
(runs "terminal_command" with $ACC_CWD and $ACC_CMD set).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			// Resolve here: the daemon's working directory isn't ours.
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(dir, "~") {
				dir = abs
			}
			req.Cwd = dir
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			resp, err := c.Launch(cx, req)
			if err != nil {
				return err
			}
			fmt.Printf("started %s in %s\n  %s\n", resp.SessionID[:8], resp.Terminal, resp.Command)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&req.Title, "title", "t", "", "session title (ACC and the agent's session name)")
	f.StringVarP(&req.Group, "group", "g", "", "group path to add the session to")
	f.StringVar(&req.Color, "color", "", "session colour (#rrggbb)")
	f.StringVarP(&req.Prompt, "prompt", "p", "", "first prompt")
	return cmd
}

func renameCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "rename <session-id-prefix> [title...]",
		Short: "Set a session's title in ACC (--clear reverts to the agent's title)",
		Long: `Sets the title ACC shows for a session, over the agent's own (/rename) and
auto-generated titles. It doesn't rename the session inside the agent.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.Join(args[1:], " ")
			if title == "" && !clear {
				return fmt.Errorf("give a title, or --clear")
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			return c.RenameSession(cx, args[0], title)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the ACC title")
	return cmd
}

func groupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "group", Short: "Manage session groups and colours"}
	run := func(fn func(args []string) error) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error { return fn(args) }
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "ls", Short: "List groups",
			RunE: run(func([]string) error {
				c, err := newClient()
				if err != nil {
					return err
				}
				cx, cancel := ctx()
				defer cancel()
				gs, err := c.Groups(cx)
				if err != nil {
					return err
				}
				for _, g := range gs {
					color := g.Color
					if color == "" {
						color = "(" + g.Display + ")"
						if g.Display == "" {
							color = "-"
						}
					}
					fmt.Printf("%-10s %s%s\n", color, strings.Repeat("  ", g.Depth), g.Name)
				}
				return nil
			}),
		},
		&cobra.Command{
			Use:   "set <path> [#rrggbb|inherit]",
			Short: "Create a group (missing parents too) or set its colour; omitted = inherit from parent",
			Args:  cobra.RangeArgs(1, 2),
			RunE: run(func(a []string) error {
				if len(a) == 1 || a[1] == "inherit" {
					a = []string{a[0], ""}
				}
				c, err := newClient()
				if err != nil {
					return err
				}
				cx, cancel := ctx()
				defer cancel()
				_, err = c.UpsertGroup(cx, a[0], a[1])
				return err
			}),
		},
		&cobra.Command{
			Use: "rm <path>", Short: "Delete a group and its subgroups", Args: cobra.ExactArgs(1),
			RunE: run(func(a []string) error {
				c, err := newClient()
				if err != nil {
					return err
				}
				cx, cancel := ctx()
				defer cancel()
				return c.DeleteGroup(cx, a[0])
			}),
		},
		&cobra.Command{
			Use: "add <group> <session>...", Short: "Add sessions to a group", Args: cobra.MinimumNArgs(2),
			RunE: run(func(a []string) error { return assignAll(a[0], a[1:], false) }),
		},
		&cobra.Command{
			Use: "remove <group> <session>...", Short: "Remove sessions from a group", Args: cobra.MinimumNArgs(2),
			RunE: run(func(a []string) error { return assignAll(a[0], a[1:], true) }),
		},
	)
	return cmd
}

func assignAll(group string, sessions []string, remove bool) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	cx, cancel := ctx()
	defer cancel()
	for _, s := range sessions {
		if err := c.Assign(cx, s, group, remove); err != nil {
			return err
		}
	}
	return nil
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check daemon connectivity and agent integrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ok := true
			check := func(name string, err error, hint string) {
				if err != nil {
					ok = false
					fmt.Printf("✗ %s: %v\n    → %s\n", name, err, hint)
					return
				}
				fmt.Printf("✓ %s\n", name)
			}

			_, err := config.Token()
			check("local token ("+config.Home()+")", err, "run `acc serve` once to create it")
			c, err := newClient()
			if err == nil {
				cx, cancel := ctx()
				_, err = c.Sessions(cx)
				cancel()
			}
			check("daemon reachable at "+config.ServerURL(), err, "start `acc serve`")

			bin, binErr := setup.ResolveBin("")
			for _, a := range setup.Adapters() {
				inst, found := a.Detect()
				if !found {
					fmt.Printf("- %s not installed\n", a.Name())
					continue
				}
				if binErr != nil {
					check(a.Name()+" hooks", binErr, "install acc first")
					continue
				}
				changes, err := a.Plan(setup.Options{Scope: setup.ScopeUser, Bin: bin})
				if err == nil {
					for _, ch := range changes {
						if !ch.Noop() {
							err = fmt.Errorf("%s is missing or outdated", ch.Path)
						}
					}
				}
				check(fmt.Sprintf("%s hooks (%s)", a.Name(), inst.Version), err, "run `acc setup`")
			}
			if !ok {
				os.Exit(1)
			}
			return nil
		},
	}
}
