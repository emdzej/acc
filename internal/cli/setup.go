package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/channel"
	"github.com/emdzej/acc/internal/hook"
	"github.com/emdzej/acc/internal/setup"
)

func hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook <agent>",
		Short:  "Handle an agent hook event (invoked by agents, not by hand)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Never fail the agent: report on stderr and exit 0.
			if err := hook.Run(args[0], os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "acc hook:", err)
			}
			return nil
		},
	}
}

func statuslineCmd() *cobra.Command {
	var chain string
	cmd := &cobra.Command{
		Use:    "statusline",
		Short:  "Agent status line command: reports plan limits and context to ACC (invoked by agents)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := hook.RunStatusline(chain, os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "acc statusline:", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&chain, "chain", "", "run this status line command and print its output instead of ACC's")
	return cmd
}

func setupCmd() *cobra.Command {
	var (
		agents              []string
		scope, dir, bin     string
		dryRun, yes, remove bool
		noStatusLine        bool
		noChannel           bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install ACC into detected agents (Claude Code hooks, OpenCode plugin)",
		Long: `Detects installed agents and merges ACC hooks into their configuration.
Existing configuration is preserved, the original file is backed up, and
re-running is idempotent. Use --uninstall to remove only ACC's entries.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := setup.Options{Scope: setup.Scope(scope), ProjectDir: dir, NoStatusLine: noStatusLine, NoChannel: noChannel}
			if opts.Scope != setup.ScopeUser && opts.Scope != setup.ScopeProject {
				return fmt.Errorf("--scope must be user or project")
			}
			var err error
			if opts.Bin, err = setup.ResolveBin(bin); err != nil && !remove {
				return err
			}

			targets, err := selectAdapters(agents)
			if err != nil {
				return err
			}
			var all []setup.Change
			var cmds []setup.Command
			fmt.Println("Agents:")
			for _, a := range targets {
				inst, ok := a.Detect()
				if !ok && len(agents) == 0 {
					fmt.Printf("  - %-12s not found, skipping\n", a.Name())
					continue
				}
				fmt.Printf("  ✓ %-12s %s\n", a.Name(), inst.Version)
				plan := a.Plan
				if remove {
					plan = a.PlanUninstall
				}
				changes, err := plan(opts)
				if err != nil {
					return fmt.Errorf("%s: %w", a.Name(), err)
				}
				for _, c := range changes {
					state := "up to date"
					if !c.Noop() {
						state = c.Note
					}
					fmt.Printf("      %s\n        %s\n", c.Path, state)
				}
				all = append(all, changes...)
				if cm, ok := a.(setup.Commander); ok {
					cs, err := cm.PlanCommands(opts, remove)
					if err != nil {
						return fmt.Errorf("%s: %w", a.Name(), err)
					}
					for _, c := range cs {
						fmt.Printf("      %s\n        %s\n", c.Note, strings.Join(c.Argv, " "))
					}
					cmds = append(cmds, cs...)
				}
			}
			targeted := func(name string) bool {
				return slices.ContainsFunc(targets, func(a setup.Adapter) bool { return a.Name() == name })
			}
			if !remove && targeted("claude-code") {
				fmt.Printf("Hook command: %s hook <agent>\n", opts.Bin)
			}

			pending := 0
			for _, c := range all {
				if !c.Noop() {
					pending++
				}
			}
			if pending == 0 && len(cmds) == 0 {
				fmt.Println("\nNothing to do.")
				return nil
			}
			if dryRun {
				printDiffs(all)
				return nil
			}
			if !yes && !confirm(all) {
				fmt.Println("Aborted.")
				return nil
			}
			if err := setup.Apply(all); err != nil {
				return err
			}
			for _, c := range cmds {
				if err := c.Run(); err != nil {
					return fmt.Errorf("%s: %w", c.Note, err)
				}
			}
			fmt.Printf("\nDone: %d file(s) updated (backups saved as *.acc-bak-<timestamp>), %d command(s) run.\n", pending, len(cmds))
			if !remove {
				fmt.Println("Restart running agent sessions to pick up the hooks, and keep `acc serve` (or `acc tui`) running.")
				if targeted("opencode") {
					fmt.Println("OpenCode loads the ACC plugin when it starts: restart OpenCode (and its background service) if it is running.")
				}
				if targeted("claude-code") && !noChannel {
					fmt.Println("Start Claude with `acc claude` (or `claude --dangerously-load-development-channels server:acc`)")
					fmt.Println("so ACC messages are delivered immediately and wake idle sessions.")
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&agents, "agent", nil, "limit to these agents ("+strings.Join(adapterNames(), ", ")+")")
	f.StringVar(&scope, "scope", "user", "user (global agent config) or project")
	f.StringVar(&dir, "project-dir", "", "project directory for --scope project (default: cwd)")
	f.StringVar(&bin, "bin", "", "acc binary path to write into hooks (default: this binary)")
	f.BoolVar(&dryRun, "dry-run", false, "show the diff without writing")
	f.BoolVarP(&yes, "yes", "y", false, "apply without prompting")
	f.BoolVar(&remove, "uninstall", false, "remove ACC entries instead of installing")
	f.BoolVar(&noChannel, "no-channel", false, "don't register the `acc mcp` message channel (instant delivery to idle sessions)")
	f.BoolVar(&noStatusLine, "no-statusline", false, "don't install the status line feed (plan limits, context %)")
	return cmd
}

func selectAdapters(names []string) ([]setup.Adapter, error) {
	if len(names) == 0 {
		return setup.Adapters(), nil
	}
	var out []setup.Adapter
	for _, n := range names {
		a, ok := setup.Lookup(n)
		if !ok {
			return nil, fmt.Errorf("unknown agent %q", n)
		}
		out = append(out, a)
	}
	return out, nil
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "Claude Code channel server: pushes ACC messages into the session (invoked by Claude)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return channel.Serve(cmd.Context(), os.Stdin, os.Stdout, os.Stderr, version)
		},
	}
}

func claudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude [claude args...]",
		Short: "Start Claude Code with ACC's message channel enabled",
		Long: `Runs claude with --dangerously-load-development-channels server:acc, so
messages sent from ACC reach the session immediately, waking it when idle.
All arguments are passed to claude, e.g. ` + "`acc claude --resume <id>`" + `.
Claude asks once per launch to confirm loading the development channel.`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bin, err := exec.LookPath("claude")
			if err != nil {
				return err
			}
			argv := append([]string{bin, "--dangerously-load-development-channels", "server:" + channel.ServerName}, args...)
			return syscall.Exec(bin, argv, os.Environ())
		},
	}
}

func printDiffs(changes []setup.Change) {
	for _, c := range changes {
		if !c.Noop() {
			fmt.Println()
			fmt.Print(setup.Diff(c))
		}
	}
}

func confirm(changes []setup.Change) bool {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nApply? [y/N/d=show diff] ")
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, os.ErrClosed) && line == "" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true
		case "d", "diff":
			printDiffs(changes)
		default:
			return false
		}
	}
}

func adapterNames() []string {
	var out []string
	for _, a := range setup.Adapters() {
		out = append(out, a.Name())
	}
	return out
}
