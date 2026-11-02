package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install ACC hooks into detected agents (Claude Code, …)",
		Long: `Detects installed agents and merges ACC hooks into their configuration.
Existing configuration is preserved, the original file is backed up, and
re-running is idempotent. Use --uninstall to remove only ACC's entries.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := setup.Options{Scope: setup.Scope(scope), ProjectDir: dir, NoStatusLine: noStatusLine}
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
			}
			if !remove {
				fmt.Printf("Hook command: %s hook <agent>\n", opts.Bin)
			}

			pending := 0
			for _, c := range all {
				if !c.Noop() {
					pending++
				}
			}
			if pending == 0 {
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
			fmt.Printf("\nDone: %d file(s) updated (backups saved as *.acc-bak-<timestamp>).\n", pending)
			if !remove {
				fmt.Println("Restart running agent sessions to pick up the hooks, and keep `acc serve` running.")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&agents, "agent", nil, "limit to these agents (claude-code)")
	f.StringVar(&scope, "scope", "user", "user (global agent config) or project")
	f.StringVar(&dir, "project-dir", "", "project directory for --scope project (default: cwd)")
	f.StringVar(&bin, "bin", "", "acc binary path to write into hooks (default: this binary)")
	f.BoolVar(&dryRun, "dry-run", false, "show the diff without writing")
	f.BoolVarP(&yes, "yes", "y", false, "apply without prompting")
	f.BoolVar(&remove, "uninstall", false, "remove ACC entries instead of installing")
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
