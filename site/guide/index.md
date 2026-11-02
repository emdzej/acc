# Getting started

ACC is a single binary. It runs a small local daemon that your agents report
to through their hooks, and it serves the dashboards that show those sessions.

## Install

ACC needs Go 1.27 or newer (there are no prebuilt releases yet):

```sh
go install github.com/emdzej/acc/cmd/acc@latest
```

This puts `acc` in `~/go/bin`. Add it to your `PATH` if it isn't there yet:

```sh
export PATH="$HOME/go/bin:$PATH"   # e.g. in ~/.zshrc
```

## Connect your agents

```sh
acc setup
```

`acc setup` finds the agents you have installed and shows exactly what it will
change before asking. For Claude Code it:

- merges ACC's hooks into `~/.claude/settings.json` without touching your own
  hooks or settings,
- sets the `statusLine` command so ACC receives plan limits and context fill.
  If you already have a status line, ACC wraps it and keeps showing its output.

It backs up the original file (`settings.json.acc-bak-<timestamp>`), and
running it again changes nothing. Useful flags:

| Flag | Effect |
|---|---|
| `--dry-run` | print the diff, write nothing |
| `--scope project` | write `.claude/settings.local.json` in the current repo instead |
| `--no-statusline` | skip the status line integration |
| `--uninstall` | remove only ACC's entries and restore your status line |

Restart running agent sessions so they load the hooks.

## Open a dashboard

```sh
acc tui     # full-screen terminal dashboard
```

If no daemon is running, `acc tui` starts one inside its own process, so this
one command is enough. That embedded server stops when you quit. Events fired
in the meantime are spooled and replayed on the next start, but messages can't
be delivered while nothing is running.

To keep ACC running in the background and use the browser:

```sh
acc serve   # daemon on 127.0.0.1:7317
acc open    # opens the web dashboard, signed in with your local token
```

`acc tui` connects to a running `acc serve` automatically.

## Check everything is wired

```sh
acc doctor
```

It checks the local token, that the daemon is reachable, and that each
detected agent's hooks are installed and current.

## Next

- [Dashboards](/guide/dashboards): the TUI panels and the web UI
- [Groups & colours](/guide/groups): organise sessions
- [Messaging](/guide/messaging): send instructions to sessions
- [Monitoring](/guide/monitoring): attention, subagents, transcripts and usage
