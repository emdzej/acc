package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/model"
)

func resumeCmd() *cobra.Command {
	var req model.ResumeRequest
	cmd := &cobra.Command{
		Use:   "resume <session>",
		Short: "Bring a session back on screen, or reopen an ended one in a new window",
		Long: `Brings a session back: its window, if ACC opened one that is still there;
otherwise a new terminal window that continues the session in its directory
(claude --resume, opencode --session), as configured for acc new.

The session can be any session the agents have on record (see acc history),
not only those ACC lists. A Claude Code session still running in a terminal
ACC didn't open can't be resumed a second time.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			resp, err := c.Resume(cx, args[0], req)
			if err != nil {
				return err
			}
			if resp.Action == model.ResumeAttached {
				fmt.Printf("%s brought back on screen\n", model.ShortID(resp.SessionID))
				return nil
			}
			fmt.Printf("resumed %s in %s\n  %s\n", model.ShortID(resp.SessionID), resp.Terminal, resp.Command)
			return nil
		},
	}
	cmd.Flags().Var(boolPtr{&req.Tmux}, "tmux", "host the session in tmux; --tmux=false for a plain window")
	cmd.Flags().Lookup("tmux").NoOptDefVal = "true"
	return cmd
}

func dirCmd(use, short, app string) *cobra.Command {
	return &cobra.Command{
		Use: use + " <session>", Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			return c.OpenDir(cx, args[0], app)
		},
	}
}

func historyCmd() *cobra.Command {
	var limit int
	var add bool
	cmd := &cobra.Command{
		Use:   "history [filter]",
		Short: "List every session the agents have on record (Claude Code transcripts, OpenCode sessions)",
		Long: `Lists the sessions Claude Code and OpenCode keep, newest first, whether or
not ACC lists them. The filter matches the title, directory or ID.

  acc history api            sessions about "api"
  acc resume <id>            reopen one in a new window
  acc history --add <id>     list it in ACC (as ended) without reopening it`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			if add {
				if len(args) != 1 {
					return errors.New("--add needs a session ID")
				}
				s, err := c.Track(cx, args[0])
				if err != nil {
					return err
				}
				fmt.Printf("%s (%s) is in ACC\n", model.ShortID(s.ID), s.Title)
				return nil
			}
			past, err := c.History(cx, limit)
			if err != nil {
				return err
			}
			q := ""
			if len(args) == 1 {
				q = strings.ToLower(args[0])
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tAGENT\tTITLE\tDIR\tACTIVE\tSTATE")
			for _, p := range past {
				if q != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.Cwd+" "+p.ID), q) {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", model.ShortID(p.ID), p.Agent, clipText(p.Title, 50),
					filepath.Base(p.Cwd), Ago(p.LastActive), pastState(p))
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "how many sessions to list")
	cmd.Flags().BoolVar(&add, "add", false, "add the given session to ACC instead of listing")
	return cmd
}

func pastState(p model.PastSession) string {
	var s []string
	if p.Running {
		s = append(s, "running")
	}
	if p.Tracked {
		s = append(s, "in ACC")
	}
	return strings.Join(s, ", ")
}

func forgetCmd() *cobra.Command {
	var ended bool
	cmd := &cobra.Command{
		Use:   "forget <session>... | --ended",
		Short: "Remove sessions from ACC (the agent keeps them; see acc history)",
		Long: `Removes sessions from ACC with their events, queued messages and group
memberships. The agent's own records (transcripts, OpenCode's database) are
not touched: acc history still lists them and acc resume reopens them, and a
session that reports in again reappears in ACC.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ended == (len(args) > 0) {
				return errors.New("give session IDs or --ended")
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			cx, cancel := ctx()
			defer cancel()
			if ended {
				n, err := c.ForgetEnded(cx)
				if err == nil {
					fmt.Printf("forgot %d ended session(s)\n", n)
				}
				return err
			}
			for _, id := range args {
				if err := c.Forget(cx, id); err != nil {
					return err
				}
			}
			fmt.Printf("forgot %d session(s)\n", len(args))
			return nil
		},
	}
	cmd.Flags().BoolVar(&ended, "ended", false, "forget every ended session")
	return cmd
}

// ago formats a unix-ms time as "5m ago", "3d ago".
func Ago(ms int64) string {
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
