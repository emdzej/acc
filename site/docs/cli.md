# CLI reference

All commands talk to the daemon at `$ACC_SERVER` (default
`http://127.0.0.1:7317`), authenticating with `$ACC_TOKEN` or `~/.acc/token`.
State lives in `$ACC_HOME` (default `~/.acc`).

## Daemon and dashboards

| Command | Description |
|---|---|
| `acc serve [--addr 127.0.0.1:7317]` | Run the daemon: API, event stream, web dashboard. Loopback addresses only. |
| `acc tui [--serve auto\|always\|never]` | Terminal dashboard. `auto` embeds a server if none is listening locally. |
| `acc open [--print]` | Open the web dashboard signed in with the local token (`--print` prints the URL). |
| `acc doctor` | Check the token, the daemon, and each agent's hooks. |

## Setup

| Command | Description |
|---|---|
| `acc setup` | Install hooks and the status line into detected agents. Shows the plan and asks first. |
| `--agent claude-code` | Limit to one agent. |
| `--scope user\|project` | User config (default) or `.claude/settings.local.json` in `--project-dir`. |
| `--dry-run` | Print the diff only. |
| `-y, --yes` | Apply without asking. |
| `--no-statusline` | Skip, or remove, the status line integration. |
| `--uninstall` | Remove ACC's entries and restore a wrapped status line. |
| `--bin <path>` | The `acc` path to write into hook commands (default: this binary). |

## Sessions and messages

| Command | Description |
|---|---|
| `acc ls` | List sessions: ID prefix, agent, title, status, last tool, last seen, groups, queued messages. |
| `acc send <session> <message…>` | Queue a message for one session (any unique ID prefix). |
| `acc send -g <group> <message…>` | Queue a message for every live session in a group and its subgroups. |
| `acc color <session> <#rrggbb\|none>` | Set a session's own colour; `none` reverts to its group colour. |

## Groups

| Command | Description |
|---|---|
| `acc group ls` | Show the tree; inherited colours appear in parentheses. |
| `acc group set <path> [#rrggbb\|inherit]` | Create a group (and missing parents) or set its colour. |
| `acc group add <path> <session>…` | Add sessions to a group. |
| `acc group remove <path> <session>…` | Remove sessions from a group. |
| `acc group rm <path>` | Delete a group and its subgroups. |

## Agent-facing (hidden)

| Command | Description |
|---|---|
| `acc hook claude-code` | Claude Code hook handler: reads the hook JSON on stdin, prints messages to inject. Always exits 0. |
| `acc statusline [--chain <cmd>]` | Claude Code status line: forwards plan limits and context, prints a status line or `<cmd>`'s output. |

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `ACC_HOME` | `~/.acc` | state directory |
| `ACC_SERVER` | `http://127.0.0.1:7317` | daemon URL for clients and hooks |
| `ACC_TOKEN` | contents of `$ACC_HOME/token` | API token |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | extra transcript root ACC may read |
