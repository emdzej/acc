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

### `acc serve` network options

| Flag | Description |
|---|---|
| `--addr <host:port>` | Listen address. Anything beyond loopback (e.g. `0.0.0.0:7317`) asks to confirm; see [Network access](/docs/security#network-access). |
| `--auth key\|none` | Beyond loopback: sign in with the remote key in `~/.acc/remote_key` (default), or no auth at all. |
| `--tls-cert <pem>` `--tls-key <pem>` | HTTPS for other machines; this machine keeps plain HTTP on the same port. |
| `--allow-host <name>` | Host names the dashboard may be reached by, besides IP addresses and this machine's name (repeatable). |
| `--yes` | Accept the exposure without asking (no terminal, service managers). |

## Setup

| Command | Description |
|---|---|
| `acc setup` | Install hooks, the status line and the channel (Claude Code) or the plugin (OpenCode) into detected agents. Shows the plan and asks first. |
| `--agent claude-code\|opencode` | Limit to these agents. |
| `--scope user\|project` | User config (default), or the project's `.claude/settings.local.json` / `.opencode/plugins/acc.js` in `--project-dir`. |
| `--dry-run` | Print the diff only. |
| `-y, --yes` | Apply without asking. |
| `--no-statusline` | Skip, or remove, the status line integration. |
| `--no-channel` | Skip, or remove, the `acc mcp` message channel (instant delivery). |
| `--uninstall` | Remove ACC's entries and restore a wrapped status line. |
| `--bin <path>` | The `acc` path to write into hook commands (default: this binary). |

## Sessions and messages

| Command | Description |
|---|---|
| `acc ls` | List sessions: ID prefix, agent, title, status, last tool, last seen, groups, queued messages. OpenCode IDs (`ses_…`) show 16 characters, since they share a long prefix. |
| `acc send <session> <message…>` | Message one session (any unique ID prefix). A single-line `/command` runs as a command: in any OpenCode session, and typed into Claude Code sessions ACC started. |
| `acc send -g <group> <message…>` | Queue a message for every live session in a group and its subgroups. |
| `acc attach <session>` | Bring a session ACC started back on screen: a new window attached to its tmux session, or its Ghostty window raised. |
| `acc resume <session> [--tmux]` | Bring a session back on screen, or reopen an ended one in a new window (`claude --resume`, `opencode --session`). Accepts any session in `acc history`. Refuses a Claude Code session still running elsewhere. |
| `acc history [filter] [-n N]` | Every session Claude Code (transcripts) and OpenCode (its database) have on record, newest first, marked *running* and *in ACC*. `--add <id>` lists one in ACC as ended. |
| `acc term <session>` / `acc files <session>` | Open a terminal window (your login shell, in the configured terminal) or Finder / the file manager in the session's directory. |
| `acc forget <session>…` / `--ended` | Remove sessions from ACC (events, messages, groups); the agent keeps them. `--ended` forgets every ended session. |
| `acc rename <session> <title…>` | Set the session's title in ACC (over `/rename` and auto titles). `--clear` reverts. Doesn't rename it inside the agent. |
| `acc claude [args…]` | Start Claude Code with ACC's channel enabled (`--dangerously-load-development-channels server:acc`). |
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
| `acc mcp` | Claude Code channel server (stdio MCP): pushes ACC messages into its session. |
| `acc statusline [--chain <cmd>]` | Claude Code status line: forwards plan limits and context, prints a status line or `<cmd>`'s output. |

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `ACC_HOME` | `~/.acc` | state directory |
| `ACC_SERVER` | `http://127.0.0.1:7317` | daemon URL for clients and hooks |
| `ACC_TOKEN` | contents of `$ACC_HOME/token` | API token |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | extra transcript root ACC may read |
