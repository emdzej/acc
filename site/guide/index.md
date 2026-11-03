# Getting started

ACC is a session manager for coding agents: it shows every session your
agents run, which ones need you, and lets you organise them and talk to
them. It doesn't orchestrate agents or split work between them; you keep
running each agent the way you do now ([how ACC compares](/docs/comparison)
with tools that do).

ACC is a single binary. It runs a small local daemon that your agents report
to through their hooks, and it serves the dashboards that show those sessions.

## Install

Download the archive for your platform (macOS or Linux, arm64 or amd64) from
[GitHub Releases](https://github.com/emdzej/acc/releases), unpack it and put
`acc` somewhere stable on your `PATH`, e.g. `~/.local/bin`. Hooks call it by
absolute path, so don't run it from a download folder you clean up.

```sh
tar -xzf acc_0.4.0_darwin_arm64.tar.gz
install -m 755 acc_0.4.0_darwin_arm64/acc ~/.local/bin/acc
```

Or build it with Go 1.27 or newer:

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
- registers `acc mcp`, the message channel that delivers ACC messages
  instantly (see [Messaging](/guide/messaging)).

For OpenCode it installs the ACC plugin,
`~/.config/opencode/plugins/acc.js`, which reports sessions to ACC and
delivers messages (see [Integrations](/docs/integrations#opencode)).
Restart OpenCode so it loads the plugin.

For Codex it adds ACC's hooks to `~/.codex/hooks.json`. Codex runs new hooks
only after you trust them, so open Codex once and approve ACC's hooks with
`/hooks` (see [Integrations](/docs/integrations#codex)).

For Copilot CLI it writes ACC's hooks to their own file,
`~/.copilot/hooks/acc.json` (see [Integrations](/docs/integrations#copilot-cli)).

For Gemini CLI it adds ACC's hooks to `~/.gemini/settings.json`. Gemini runs
hooks only in folders you trust (see [Integrations](/docs/integrations#gemini-cli)).

For pi it installs the ACC extension, `~/.pi/agent/extensions/acc.js`, which
does the same (see [Integrations](/docs/integrations#pi)). Restart running
pi sessions, or `/reload` them.

It backs up every file it changes (for example
`settings.json.acc-bak-<timestamp>`), and running it again changes nothing. Useful flags:

| Flag | Effect |
|---|---|
| `--dry-run` | print the diff, write nothing |
| `--agent claude-code` | limit to one agent (`claude-code`, `opencode`, `pi`, `codex`, `copilot`, `gemini`) |
| `--scope project` | write `.claude/settings.local.json` (Claude Code), `.opencode/plugins/acc.js` (OpenCode), `.pi/extensions/acc.js` (pi), `.codex/hooks.json` (Codex), `.github/hooks/acc.json` (Copilot CLI) or `.gemini/settings.json` (Gemini CLI) in the current repo instead |
| `--no-statusline` | skip the status line integration |
| `--no-channel` | skip the message channel |
| `--uninstall` | remove only ACC's entries and restore your status line |

Restart running agent sessions so they load the hooks. To have Claude Code
messages arrive instantly (and wake idle sessions), start Claude with
`acc claude`, or alias it once (OpenCode and pi need nothing extra):

```sh
alias claude='claude --dangerously-load-development-channels server:acc'
```

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
- [Starting sessions](/guide/launching): new sessions in a terminal window or tmux
- [Monitoring](/guide/monitoring): attention, subagents, goals, transcripts, usage and limits

To look around before wiring in your own agents, `scripts/demo.sh` in a
checkout starts a throwaway daemon with made-up sessions.
