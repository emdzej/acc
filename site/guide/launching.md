# Starting sessions

ACC can start a new Claude Code session for you: pick a directory, and
optionally a group, a title and a first prompt. ACC opens a terminal window
running the session, already grouped, titled and with the
[message channel](/guide/messaging#instantly-over-a-channel) enabled.

| Where | How |
|---|---|
| TUI | `N`: directory (defaults to the selected session's), then title, then first prompt. The session joins the group selected in the Groups panel. |
| Web UI | **+ Session** above the session list. |
| CLI | `acc new [dir] [-t title] [-g group] [--color #rrggbb] [-p prompt]` |

```sh
acc new ~/Projects/api -t "fix flaky tests" -g team/api -p "run the test suite and fix failures"
```

Each new window asks you once to confirm loading the development channel
(and, for a directory Claude Code hasn't seen, to trust it).

## How it works

ACC picks the session ID up front and runs
`claude … --session-id <id> --name <title> -- <prompt>`, so the session's
group, title and colour are in place before its first hook arrives. Until
then it shows as **starting**; a launch that never reports in (window
closed, dialog declined) counts as ended after 10 minutes.

The window opens on the machine running the ACC daemon.

## Configuration

`~/.acc/config.json`, re-read on every launch:

```json
{
  "launch": {
    "terminal": "ghostty",
    "command": "claude --dangerously-load-development-channels server:acc"
  }
}
```

| Key | Values |
|---|---|
| `terminal` | `ghostty` (default when installed; Ghostty 1.3+, via AppleScript), `terminal` (Terminal.app, the fallback), `iterm`, `tmux` (new window, or a detached `acc` session), `custom` |
| `command` | The agent command line. ACC appends `--session-id`, `--name` and the prompt. Add your usual flags here, e.g. `--dangerously-skip-permissions`. |
| `terminal_command` | For `custom`: a shell command run with `$ACC_CWD`, `$ACC_CMD` and `$ACC_SESSION_ID` set. |

The command runs in a login, interactive shell (`$SHELL -lic`), so your
usual `PATH` applies. The first launch through Terminal, iTerm or Ghostty may
ask macOS to allow ACC to control that app.

::: warning
Only ACC's own config decides what runs: API callers choose the directory,
title, group and prompt, never the command. Starting a session is still an
instruction to an agent that can run code, and is recorded in the daemon's
audit log.
:::
