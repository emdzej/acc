// Command acc is the Agents Control Centre: daemon, TUI, CLI and agent hook handler in one binary.
package main

import (
	"os"

	"github.com/emdzej/acc/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
