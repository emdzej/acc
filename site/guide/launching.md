# Starting sessions

ACC can start a new Claude Code session for you: pick a directory, and
optionally a group, a title and a first prompt. ACC opens a terminal window
running the session, already grouped, titled and with the
[message channel](/guide/messaging#instantly-over-a-channel) enabled.
Starting OpenCode sessions from ACC isn't supported yet; start them as usual
and the ACC plugin reports them.

| Where | How |
|---|---|
| TUI | `N`: directory (defaults to the selected session's), then title, then first prompt, then whether to host it in tmux. The session joins the group you're in on the tree. |
| Web UI | **+ Session** above the session list. |
| CLI | `acc new [dir] [-t title] [-g group] [--color #rrggbb] [-p prompt] [--tmux]` |

```sh
acc new ~/Projects/api -t "fix flaky tests" -g team/api -p "run the test suite and fix failures"
```

Each new window asks you once to confirm loading the development channel
(and, for a directory Claude Code hasn't seen, to trust it).

## Window or tmux

By default the session runs directly in a new terminal window. With tmux
(`--tmux`, the TUI's last question, **Host** in the web form, or
`"tmux": true` in the config), the session runs in its own detached tmux
session, `acc-<id>`, and the window is attached to it:

- **It survives the window.** Close the window, quit the terminal app or log
  out of SSH, and the session keeps running. Bring it back with `a` in the
  TUI, **↗ bring back** in the web UI, `acc resume <session>`, or plain
  `tmux attach -t acc-<id>`. Once a session has ended, the same `a` or
  `acc resume` [reopens it](/guide/history#resume-a-session) in a new window.
- **You can still use your terminal's own tabs, fonts and scrollback** for
  everything else; only ACC's sessions live in tmux.

Either way, sessions ACC starts accept [slash commands](/guide/messaging#slash-commands)
from ACC: it types them into the tmux session or the Ghostty window. For a
plain Ghostty window, "bring back" raises it.

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
| `terminal` | `ghostty` (default when installed; Ghostty 1.3+, via AppleScript), `terminal` (Terminal.app, the fallback), `iterm`, `tmux` (headless: a detached tmux session and no window), `custom` |
| `tmux` | `true` to host sessions in tmux with `terminal` opening an attached window (see above). Overridden per launch. |
| `command` | The agent command line. ACC appends `--session-id`, `--name` and the prompt. Add your usual flags here, e.g. `--dangerously-skip-permissions`. |
| `opencode_command` | The command that [resumes](/guide/history#resume-a-session) OpenCode sessions (default `opencode`); ACC appends `--session <id>`. |
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
