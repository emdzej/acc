// Package cli wires ACC's subcommands.
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/client"
	"github.com/emdzej/acc/internal/model"
)

var version = "dev"

func Execute() error {
	root := &cobra.Command{
		Use:           "acc",
		Short:         "Agents Control Centre: watch, organise and interact with your coding agents' sessions",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(
		serveCmd(), hookCmd(), statuslineCmd(), mcpCmd(), setupCmd(), claudeCmd(), tuiCmd(), openCmd(),
		sessionsCmd(), newCmd(), attachCmd(), resumeCmd(), historyCmd(), forgetCmd(),
		dirCmd("term", "Open a terminal window in the session's directory", model.OpenTerminal),
		dirCmd("files", "Show the session's directory in Finder / the file manager", model.OpenFiles), sendCmd(), renameCmd(), colorCmd(), groupCmd(), doctorCmd(),
	)
	return root.Execute()
}

func newClient() (*client.Client, error) { return client.FromEnv(10 * time.Second) }

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
