// Package cli wires ACC's subcommands.
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/client"
)

var version = "dev"

func Execute() error {
	root := &cobra.Command{
		Use:           "acc",
		Short:         "Agents Control Centre: monitor and message your coding agents' sessions",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(
		serveCmd(), hookCmd(), statuslineCmd(), setupCmd(), tuiCmd(), openCmd(),
		sessionsCmd(), sendCmd(), colorCmd(), groupCmd(), doctorCmd(),
	)
	return root.Execute()
}

func newClient() (*client.Client, error) { return client.FromEnv(10 * time.Second) }

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
